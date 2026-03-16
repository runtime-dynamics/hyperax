package nervous

import (
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestMatchEventType(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		eventType types.EventType
		want      bool
	}{
		// Wildcard
		{"wildcard matches everything", "*", "pipeline.start", true},
		{"wildcard matches empty", "*", "", true},

		// Exact match
		{"exact match", "cron.fire", "cron.fire", true},
		{"exact mismatch", "cron.fire", "cron.complete", false},

		// Prefix wildcard (pipeline.*)
		{"prefix wildcard match", "pipeline.*", "pipeline.start", true},
		{"prefix wildcard match complete", "pipeline.*", "pipeline.complete", true},
		{"prefix wildcard no match", "pipeline.*", "cron.fire", false},
		{"prefix wildcard partial", "pipeline.*", "pipeline.", true},

		// Suffix wildcard (*.completed)
		{"suffix wildcard match", "*.completed", "pipeline.completed", true},
		{"suffix wildcard match cron", "*.completed", "cron.completed", true},
		{"suffix wildcard no match", "*.completed", "pipeline.start", false},

		// Multi-segment
		{"multi segment prefix", "nervous.*", "nervous.drift_detected", true},
		{"multi segment prefix no match", "nervous.*", "pipeline.start", false},

		// Edge cases
		{"empty pattern exact match", "", "", true},
		{"empty pattern no match", "", "anything", false},
		{"pattern with no dots", "test", "test", true},
		{"pattern with no dots no match", "test", "other", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchEventType(tt.pattern, tt.eventType)
			if got != tt.want {
				t.Errorf("MatchEventType(%q, %q) = %v, want %v",
					tt.pattern, tt.eventType, got, tt.want)
			}
		})
	}
}

func TestMakeFilterFunc(t *testing.T) {
	filter := MakeFilterFunc("pipeline.*")

	matchEvent := types.NervousEvent{Type: types.EventPipelineStart}
	noMatchEvent := types.NervousEvent{Type: types.EventCronFire}

	if !filter(matchEvent) {
		t.Error("filter should match pipeline.start")
	}
	if filter(noMatchEvent) {
		t.Error("filter should not match cron.fire")
	}
}

func TestMakeFilterFunc_Wildcard(t *testing.T) {
	filter := MakeFilterFunc("*")

	events := []types.NervousEvent{
		{Type: types.EventPipelineStart},
		{Type: types.EventCronFire},
		{Type: types.EventMCPRequest},
		{Type: types.EventCommMessage},
	}

	for _, e := range events {
		if !filter(e) {
			t.Errorf("wildcard filter should match %s", e.Type)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"*", "anything", true},
		{"a*b", "ab", true},
		{"a*b", "aXb", true},
		{"a*b", "aXYZb", true},
		{"a*b", "aXYZc", false},
		{"*mid*", "startmidend", true},
		{"*mid*", "mid", true},
		{"*mid*", "nomatch", false},
		{"pre*mid*suf", "premidsuf", true},
		{"pre*mid*suf", "preXmidYsuf", true},
		{"pre*mid*suf", "preXYZ", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.text, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.text)
			if got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v",
					tt.pattern, tt.text, got, tt.want)
			}
		})
	}
}
