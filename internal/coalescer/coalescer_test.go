package coalescer

import (
	"context"
	"errors"
	"testing"
)

// Single · 1 人 bus 意图 · pass-through
func TestSingle_PassThrough(t *testing.T) {
	in := Intent{
		PassengerID:         "p1",
		BusID:               "b1",
		Count:               3,
		IdempotencyRecordID: "i1",
	}
	batch, err := Single(context.Background(), in)
	if err != nil {
		t.Fatalf("Single: %v", err)
	}
	if batch.BusID != "b1" || batch.CountTotal != 3 {
		t.Errorf("BusID/Count 不对 · got=%+v", batch)
	}
	if len(batch.Participants) != 1 || batch.Participants[0] != "p1" {
		t.Errorf("Participants=%v · want [p1]", batch.Participants)
	}
	if len(batch.IdempotencyRecordIDs) != 1 || batch.IdempotencyRecordIDs[0] != "i1" {
		t.Errorf("IdempotencyRecordIDs=%v · want [i1]", batch.IdempotencyRecordIDs)
	}
}

// Anon · 1c-2 才做 · 骨架期返 ErrNotImplemented
func TestAnon_ReturnsNotImplemented(t *testing.T) {
	_, err := Anon(context.Background(), Intent{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Anon 应返 ErrNotImplemented · got=%v", err)
	}
}

// Team · 2a 才做 · 骨架期返 ErrNotImplemented
func TestTeam_ReturnsNotImplemented(t *testing.T) {
	_, err := Team(context.Background(), Intent{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Team 应返 ErrNotImplemented · got=%v", err)
	}
}
