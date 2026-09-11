package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/ai"
)

func TestChunkSegmentsMergesSmallSegments(t *testing.T) {
	segs := []ai.Segment{
		{StartMs: 0, EndMs: 1000, Text: "hello world"},
		{StartMs: 1000, EndMs: 2000, Text: "second line"},
		{StartMs: 2000, EndMs: 3000, Text: "third line"},
	}
	chunks := chunkSegments(segs)
	if len(chunks) != 1 {
		t.Fatalf("want 1 merged chunk, got %d", len(chunks))
	}
	c := chunks[0]
	if c.StartMs != 0 || c.EndMs != 3000 {
		t.Fatalf("timestamps not preserved: %d..%d", c.StartMs, c.EndMs)
	}
	if !strings.Contains(c.Text, "hello world") || !strings.Contains(c.Text, "third line") {
		t.Fatalf("text lost in merge: %q", c.Text)
	}
}

func TestChunkSegmentsRespectsTargetSize(t *testing.T) {
	seg := ai.Segment{StartMs: 0, EndMs: 5000, Text: strings.Repeat("word ", 500)} // 2500 runes
	chunks := chunkSegments([]ai.Segment{seg})
	if len(chunks) < 2 {
		t.Fatalf("long input should split, got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c.Text); n > chunkHardMax {
			t.Fatalf("chunk %d exceeds hard max: %d", i, n)
		}
	}
	if chunks[0].StartMs != 0 || chunks[len(chunks)-1].EndMs != 5000 {
		t.Fatalf("span not preserved: %d..%d", chunks[0].StartMs, chunks[len(chunks)-1].EndMs)
	}
}
