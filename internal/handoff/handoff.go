package handoff

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"devctl/internal/model"
)

func FromReport(report model.Report) model.FailurePacket {
	packet := model.FailurePacket{SchemaVersion: "1", RunID: report.RunID, Overall: report.Overall, DevctlVersion: report.DevctlVersion, DevctlCommit: report.DevctlCommit, EvidencePath: report.EvidencePath, NextAction: "Inspect the listed evidence, make a source change if needed, then run devctl verify again."}
	if report.Project != nil {
		packet.Project = report.Project.Name
	}
	for _, check := range report.Checks {
		if check.Status == model.Pass || check.Status == model.Warn && !check.Blocking {
			continue
		}
		item := model.FailureItem{CheckID: check.ID, Status: check.Status, Blocking: check.Blocking, Summary: check.Summary, Reason: check.Reason, Findings: check.Findings}
		for _, evidence := range check.Evidence {
			if evidence.Path != "" {
				item.EvidencePaths = append(item.EvidencePaths, evidence.Path)
			}
		}
		packet.Failures = append(packet.Failures, item)
	}
	return packet
}

func Read(path string) (model.FailurePacket, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.FailurePacket{}, err
	}
	var report model.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return model.FailurePacket{}, fmt.Errorf("parse report: %w", err)
	}
	return FromReport(report), nil
}

func Text(packet model.FailurePacket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "RUN: %s\nOVERALL: %s\n", packet.RunID, packet.Overall)
	if packet.Project != "" {
		fmt.Fprintf(&b, "PROJECT: %s\n", packet.Project)
	}
	for _, item := range packet.Failures {
		fmt.Fprintf(&b, "- %s: %s — %s\n", item.CheckID, item.Status, item.Summary)
		if item.Reason != "" {
			fmt.Fprintf(&b, "  reason: %s\n", item.Reason)
		}
	}
	fmt.Fprintf(&b, "NEXT: %s\n", packet.NextAction)
	return b.String()
}
