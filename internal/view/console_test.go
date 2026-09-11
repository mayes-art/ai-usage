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
