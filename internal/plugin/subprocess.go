package plugin

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// shutdownGracePeriod is how long to wait after closing stdin before SIGTERM.
	shutdownGracePeriod = 5 * time.Second

	// killGracePeriod is how long to wait after SIGTERM before SIGKILL.
	killGracePeriod = 5 * time.Second

	// maxRestartAttempts is the maximum number of automatic restart attempts
	// after a crash before the plugin is marked as errored.
	maxRestartAttempts = 3
)

// Subprocess manages an external plugin process communicating over stdio.
// It owns the exec.Cmd, stdin/stdout pipes, and restart logic.
type Subprocess struct {
	manifest types.PluginManifest
	resolver *PluginConfigResolver
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	logger   *slog.Logger

	// cancel stops the context for the current process lifecycle.
	cancel context.CancelFunc

	// restartCount tracks consecutive restart attempts.
	restartCount atomic.Int32

	// running indicates whether the subprocess is currently alive.
	running atomic.Bool

	// onCrash is called when the subprocess exits unexpectedly.
	// The callback receives the plugin name and the exit error.
	onCrash func(pluginName string, err error)

	mu sync.Mutex
}

// StartSubprocess launches the plugin entrypoint as a child process, wiring
// stdin/stdout for MCP JSON-RPC communication and stderr for logging.
//
// The manifest.Entrypoint is the command to execute. manifest.Args are appended
// as arguments. Plugin variables are resolved via the PluginConfigResolver and
// injected as environment variables. Legacy manifest.Env entries are used as
// fallback when no Variables are defined.
//
// The resolver may be nil (variables use defaults and OS env only).
//
// Returns a Subprocess handle that must be stopped via Stop() when done.
func StartSubprocess(ctx context.Context, manifest types.PluginManifest, resolver *PluginConfigResolver, logger *slog.Logger) (*Subprocess, error) {
	if manifest.Entrypoint == "" {
		return nil, fmt.Errorf("plugin.StartSubprocess: plugin %q has no entrypoint", manifest.Name)
	}

	sp := &Subprocess{
		manifest: manifest,
		resolver: resolver,
		logger:   logger.With("component", "subprocess", "plugin", manifest.Name),
	}

	if err := sp.start(ctx); err != nil {
		return nil, err
	}

	return sp, nil
}

// start creates and starts the exec.Cmd. Called both for initial launch and restarts.
func (sp *Subprocess) start(ctx context.Context) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	childCtx, cancel := context.WithCancel(ctx)
	sp.cancel = cancel

	cmd := exec.CommandContext(childCtx, sp.manifest.Entrypoint, sp.manifest.Args...)

	// Merge environment: inherit current env + resolved plugin variables.
	cmd.Env = os.Environ()
	if err := sp.buildPluginEnv(childCtx, cmd); err != nil {
		cancel()
		return err
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("plugin.Subprocess.start: stdin pipe for %q: %w", sp.manifest.Name, err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("plugin.Subprocess.start: stdout pipe for %q: %w", sp.manifest.Name, err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("plugin.Subprocess.start: stderr pipe for %q: %w", sp.manifest.Name, err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("plugin.Subprocess.start: exec %q: %w", sp.manifest.Name, err)
	}

	sp.cmd = cmd
	sp.stdin = stdin
	sp.stdout = stdout
	sp.stderr = stderr
	sp.running.Store(true)

	sp.logger.Info("subprocess started", "pid", cmd.Process.Pid)

	// Drain stderr to logger.
	go sp.drainStderr()

	// Monitor process exit.
	go sp.monitor(ctx)

	return nil
}

// drainStderr reads plugin stderr and logs each line at Info level.
func (sp *Subprocess) drainStderr() {
	buf := make([]byte, 4096)
	for {
		n, err := sp.stderr.Read(buf)
		if n > 0 {
			sp.logger.Info("plugin stderr", "output", string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

// monitor waits for the process to exit and triggers crash handling if unexpected.
func (sp *Subprocess) monitor(ctx context.Context) {
	err := sp.cmd.Wait()
	sp.running.Store(false)

	select {
	case <-ctx.Done():
		// Shutdown was requested — not a crash.
		sp.logger.Info("subprocess exited after shutdown")
		return
	default:
	}

	// Unexpected exit — this is a crash.
	sp.logger.Error("subprocess crashed",
		"error", err,
		"restart_count", sp.restartCount.Load(),
	)

	if sp.onCrash != nil {
		sp.onCrash(sp.manifest.Name, err)
	}

	// Attempt automatic restart if under the limit.
	count := sp.restartCount.Add(1)
	if int(count) <= maxRestartAttempts {
		sp.logger.Info("attempting restart",
			"attempt", count,
			"max", maxRestartAttempts,
		)
		// Brief delay before restart to avoid tight loops.
		time.Sleep(time.Duration(count) * time.Second)
		if restartErr := sp.start(ctx); restartErr != nil {
			sp.logger.Error("restart failed", "error", restartErr)
		}
	} else {
		sp.logger.Error("max restart attempts exceeded, plugin will remain stopped",
			"attempts", count,
		)
	}
}

// Stdin returns the writer connected to the subprocess stdin.
// Used by MCPClient to send JSON-RPC requests.
func (sp *Subprocess) Stdin() io.Writer {
	return sp.stdin
}

// Stdout returns the reader connected to the subprocess stdout.
// Used by MCPClient to read JSON-RPC responses/notifications.
func (sp *Subprocess) Stdout() io.Reader {
	return sp.stdout
}

// Running reports whether the subprocess is currently alive.
func (sp *Subprocess) Running() bool {
	return sp.running.Load()
}

// ResetRestartCount resets the consecutive restart counter (e.g., after a
// successful health check confirms the plugin is stable).
func (sp *Subprocess) ResetRestartCount() {
	sp.restartCount.Store(0)
}

// buildPluginEnv resolves plugin variables and injects them as environment
// variables. For secret variables, the value is resolved from the secret store.
// For non-secret variables, values come from config or defaults. OS env overrides
// all. Legacy Env entries are used as fallback when no Variables are defined.
func (sp *Subprocess) buildPluginEnv(ctx context.Context, cmd *exec.Cmd) error {
	pluginName := sp.manifest.Name

	// Process typed Variables if present.
	if len(sp.manifest.Variables) > 0 {
		for _, v := range sp.manifest.Variables {
			envName := v.EnvName
			if envName == "" {
				envName = v.Name
			}

			// OS env always wins.
			if envVal, ok := os.LookupEnv(envName); ok {
				cmd.Env = append(cmd.Env, envName+"="+envVal)
				continue
			}

			var val string

			if sp.resolver != nil && sp.resolver.GetVar != nil {
				configVal, err := sp.resolver.GetVar(ctx, pluginName, v.Name)
				if err == nil && configVal != "" {
					if v.Secret && sp.resolver.ResolveSecret != nil {
						// Config stores a reference like "secret:KEY:SCOPE" — resolve it.
						resolved, resolveErr := sp.resolver.ResolveSecret(ctx, configVal)
						if resolveErr != nil {
							return fmt.Errorf("plugin.Subprocess.buildPluginEnv: resolve secret for variable %q in plugin %q: %w",
								v.Name, pluginName, resolveErr)
						}
						val = resolved
					} else {
						val = configVal
					}
				}
			}

			// Fall back to default if no config value.
			if val == "" && v.Default != nil {
				val = fmt.Sprintf("%v", v.Default)
			}

			if val == "" && v.Required {
				return fmt.Errorf("plugin.Subprocess.buildPluginEnv: required variable %q for plugin %q has no value", v.Name, pluginName)
			}

			if val != "" {
				cmd.Env = append(cmd.Env, envName+"="+val)
			}
		}
		return nil
	}

	// Legacy Env fallback.
	for _, e := range sp.manifest.Env {
		val := e.Default
		if envVal, ok := os.LookupEnv(e.Name); ok {
			val = envVal
		}
		if val != "" {
			cmd.Env = append(cmd.Env, e.Name+"="+val)
		}
	}
	return nil
}

// Stop performs a graceful shutdown sequence:
//  1. Close stdin (signals EOF to the plugin)
//  2. Wait shutdownGracePeriod for voluntary exit
//  3. Send SIGTERM, wait killGracePeriod
//  4. Send SIGKILL as last resort
func (sp *Subprocess) Stop() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.cancel != nil {
		sp.cancel()
	}

	if sp.cmd == nil || sp.cmd.Process == nil {
		return nil
	}

	if !sp.running.Load() {
		return nil
	}

	sp.logger.Info("stopping subprocess")

	// Step 1: close stdin.
	_ = sp.stdin.Close()

	// Step 2: wait for voluntary exit.
	exitCh := make(chan error, 1)
	go func() {
		exitCh <- sp.cmd.Wait()
	}()

	select {
	case <-exitCh:
		sp.running.Store(false)
		sp.logger.Info("subprocess exited gracefully")
		return nil
	case <-time.After(shutdownGracePeriod):
	}

	// Step 3: SIGTERM.
	sp.logger.Info("sending SIGTERM")
	if err := sp.cmd.Process.Signal(os.Interrupt); err != nil {
		// Process may have already exited — that is fine.
		sp.logger.Debug("sigterm failed", "error", err)
	}

	select {
	case <-exitCh:
		sp.running.Store(false)
		sp.logger.Info("subprocess exited after SIGTERM")
		return nil
	case <-time.After(killGracePeriod):
	}

	// Step 4: SIGKILL.
	sp.logger.Warn("sending SIGKILL")
	if err := sp.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("plugin.Subprocess.Stop: kill: %w", err)
	}

	<-exitCh
	sp.running.Store(false)
	sp.logger.Info("subprocess killed")
	return nil
}
