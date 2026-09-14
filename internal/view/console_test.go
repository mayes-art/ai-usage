package view

import (
	"bytes"
	"strings"
	"testing"

	"aiusage/internal/model"
)

func TestReportWritesSnapshotToChosenOutput(t *testing.T) {
	var output bytes.Buffer
	Report(&output, model.Snapshot{Host: "fixture-host", Providers: []model.ProviderSnapshot{{
		Name: "Test provider", Metric: "tokens", Used: 500, Limit: 1000,
		HasLimit: true, ShowLimit: true, Percent: 0.5,
	}}})
	for _, want := range []string{"fixture-host", "Test provider", "50%", "500 / 1.0k"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("report missing %q: %s", want, output.String())
		}
	}
}

func TestReportShowsIndependentPoolsAndUnknown(t *testing.T) {
	var output bytes.Buffer
	zero := 0.0
	Report(&output, model.Snapshot{Providers: []model.ProviderSnapshot{{Name: "Cursor", UsageGroups: []model.UsageGroup{
		{Label: "Cursor model", Percent: &zero}, {Label: "Other model"},
	}}}})
	for _, want := range []string{"Cursor model: 0%", "Other model: —%"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
}
