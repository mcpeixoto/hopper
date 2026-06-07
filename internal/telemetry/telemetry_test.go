package telemetry

import "testing"

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"123MB":  123,
		"1.5GB":  1536,
		"900kB":  0, // <1MB rounds to 0
		"2GB":    2048,
		"512B":   0,
		"  7MB ": 7,
	}
	for in, want := range cases {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q)=%d want %d", in, got, want)
		}
	}
}

func TestCollectShape(t *testing.T) {
	imgs := []Image{{Repo: "alpine:3.20", SizeMB: 7}}
	s := Collect(t.TempDir(), 4, 2, imgs)
	if s.CPUs < 1 {
		t.Fatal("expected at least 1 cpu")
	}
	if s.Slots != 4 || s.Running != 2 {
		t.Fatalf("slots/running not carried: %d/%d", s.Slots, s.Running)
	}
	if len(s.Images) != 1 || s.Images[0].Repo != "alpine:3.20" {
		t.Fatalf("images not carried: %#v", s.Images)
	}
	if s.CollectedAt == "" {
		t.Fatal("missing timestamp")
	}
	// DiskFreeMB should be >0 on a real filesystem (the temp dir).
	if s.DiskFreeMB <= 0 {
		t.Fatalf("expected positive disk free, got %d", s.DiskFreeMB)
	}
}
