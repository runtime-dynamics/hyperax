package telemetry

import (
	"math"
	"testing"
)

func TestEstimateCost_KnownTool(t *testing.T) {
	cost := EstimateCost("search_code", 0, 0)
	if cost != DefaultCostRates["search_code"] {
		t.Errorf("expected %f, got %f", DefaultCostRates["search_code"], cost)
	}
}

func TestEstimateCost_UnknownTool(t *testing.T) {
	cost := EstimateCost("completely_unknown_tool", 0, 0)
	if cost != DefaultCostRates["default"] {
		t.Errorf("expected default rate %f, got %f", DefaultCostRates["default"], cost)
	}
}

func TestEstimateCost_IOSurcharge(t *testing.T) {
	baseCost := EstimateCost("search_code", 0, 0)
	withIO := EstimateCost("search_code", 10000, 5000)

	if withIO <= baseCost {
		t.Errorf("expected IO surcharge to increase cost: base=%f, withIO=%f", baseCost, withIO)
	}

	// Expected surcharge: 15000 * 0.0000001 = 0.0015
	expectedSurcharge := 15000.0 * 0.0000001
	expectedTotal := baseCost + expectedSurcharge

	if math.Abs(withIO-expectedTotal) > 1e-10 {
		t.Errorf("expected %f, got %f", expectedTotal, withIO)
	}
}

func TestEstimateCost_ZeroIO(t *testing.T) {
	cost := EstimateCost("get_file_content", 0, 0)
	expected := DefaultCostRates["get_file_content"]
	if cost != expected {
		t.Errorf("expected %f, got %f", expected, cost)
	}
}

func TestEstimateCost_WriteToolsHigherThanReadTools(t *testing.T) {
	readCost := EstimateCost("get_file_content", 0, 0)
	writeCost := EstimateCost("replace_lines", 0, 0)

	if writeCost <= readCost {
		t.Errorf("write tools should cost more than read tools: read=%f, write=%f", readCost, writeCost)
	}
}

func TestEstimateCost_PipelineToolsHighest(t *testing.T) {
	searchCost := EstimateCost("search_code", 0, 0)
	pipelineCost := EstimateCost("run_pipeline", 0, 0)

	if pipelineCost <= searchCost {
		t.Errorf("pipeline tools should cost more: search=%f, pipeline=%f", searchCost, pipelineCost)
	}
}

func TestDefaultCostRates_HasDefault(t *testing.T) {
	_, ok := DefaultCostRates["default"]
	if !ok {
		t.Error("DefaultCostRates must have a 'default' key")
	}
}

func TestDefaultCostRates_AllPositive(t *testing.T) {
	for name, rate := range DefaultCostRates {
		if rate <= 0 {
			t.Errorf("rate for %q must be positive, got %f", name, rate)
		}
	}
}
