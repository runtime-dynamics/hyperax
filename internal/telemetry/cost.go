package telemetry

// DefaultCostRates maps tool names to their estimated cost per invocation.
// Costs are expressed in abstract cost units (ACU) and can be tuned via
// configuration. Categories are derived from common MCP tool patterns.
var DefaultCostRates = map[string]float64{
	// Code search and discovery
	"search_code":       0.001,
	"get_code_outline":  0.0008,
	"get_dependencies":  0.0008,
	"search_imports":    0.0008,

	// File operations
	"get_file_content":  0.0005,
	"list_files_in_dir": 0.0003,

	// Refactoring operations (higher cost — writes)
	"replace_lines":      0.002,
	"insert_code_block":  0.002,
	"delete_code_block":  0.0015,
	"extract_code_block": 0.0015,
	"move_symbol":        0.003,
	"ensure_imports":     0.001,

	// Documentation
	"search_docs":       0.0008,
	"list_docs":         0.0003,
	"get_doc_content":   0.0005,
	"create_doc":        0.001,
	"update_doc_content": 0.001,

	// Project management
	"list_tasks":          0.0003,
	"add_task":            0.0005,
	"update_task_status":  0.0005,

	// Pipeline operations (compute-heavy)
	"run_pipeline":     0.005,
	"run_build":        0.005,
	"run_test":         0.005,

	// Log and metrics queries
	"get_log_errors":  0.0005,
	"get_log_lines":   0.0005,
	"get_metrics":     0.0003,

	// Fallback default
	"default": 0.001,
}

// EstimateCost returns the estimated cost for a tool invocation based on the
// tool name and the sizes of the input/output payloads. The base cost comes
// from DefaultCostRates (falling back to "default"), with a small surcharge
// proportional to the combined payload size.
func EstimateCost(toolName string, inputSize, outputSize int64) float64 {
	baseCost, ok := DefaultCostRates[toolName]
	if !ok {
		baseCost = DefaultCostRates["default"]
	}

	// Add a small I/O surcharge: 0.0000001 per byte of combined payload.
	// This ensures that large payloads contribute to the cost estimate.
	ioSurcharge := float64(inputSize+outputSize) * 0.0000001

	return baseCost + ioSurcharge
}
