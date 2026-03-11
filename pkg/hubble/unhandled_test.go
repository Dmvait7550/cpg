package hubble

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestTrack_Dedup(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "production",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 8080},
			},
		},
	}

	tracker.Track(flow, "no_l4")
	tracker.Track(flow, "no_l4") // duplicate

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 1, "duplicate flow should only produce one DEBUG log")
}

func TestTrack_DifferentFlows(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow1 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "production",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 8080},
			},
		},
	}

	flow2 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=api"},
			Namespace: "staging",
		},
		Destination: &flowpb.Endpoint{
			Labels: []string{"reserved:world"},
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 443},
			},
		},
	}

	tracker.Track(flow1, "no_l4")
	tracker.Track(flow2, "world_no_ip")

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 2, "different flows should produce separate DEBUG logs")
}

func TestTrack_SameFlowDifferentReason(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "production",
		},
	}

	tracker.Track(flow, "no_l4")
	tracker.Track(flow, "unknown_protocol")

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 2, "same flow with different reasons should produce separate DEBUG logs")
}

// filterLogs returns log entries matching the given level and message substring.
func filterLogs(logs *observer.ObservedLogs, level zapcore.Level, msgSubstring string) []observer.LoggedEntry {
	var result []observer.LoggedEntry
	for _, entry := range logs.All() {
		if entry.Level == level && contains(entry.Message, msgSubstring) {
			result = append(result, entry)
		}
	}
	return result
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
