package upgrade

import "testing"

func TestParseLine(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "8.10", want: "8.10"},
		{in: "8.9.3", want: "8.9"},
		{in: "camunda-platform-8.8", want: "8.8"},
		{in: " 8.7 ", want: "8.7"},
		{in: "latest", wantErr: true},
		{in: "", wantErr: true},
	} {
		got, err := ParseLine(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseLine(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLine(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("ParseLine(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestHops(t *testing.T) {
	from, to := Line{8, 7}, Line{8, 10}
	hops, err := Hops(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(hops) != 3 {
		t.Fatalf("Hops(8.7, 8.10) = %v, want 3 hops", hops)
	}
	if hops[0].String() != "8.8" || hops[2].String() != "8.10" {
		t.Fatalf("Hops(8.7, 8.10) = %v, want 8.8 -> 8.9 -> 8.10", hops)
	}
}

func TestHopsSameVersionIsEmpty(t *testing.T) {
	hops, err := Hops(Line{8, 9}, Line{8, 9})
	if err != nil || len(hops) != 0 {
		t.Fatalf("Hops(8.9, 8.9) = %v, %v; want empty, nil", hops, err)
	}
}

func TestHopsRejectsDowngrade(t *testing.T) {
	if _, err := Hops(Line{8, 10}, Line{8, 9}); err == nil {
		t.Fatal("Hops accepted a downgrade")
	}
}

func TestChartVersionRoundTrip(t *testing.T) {
	line, ok := LineFromChartVersion("14.8.5")
	if !ok || line.String() != "8.9" {
		t.Fatalf("LineFromChartVersion(14.8.5) = %s, %v; want 8.9, true", line, ok)
	}
	major, ok := ChartMajorForLine(line)
	if !ok || major != 14 {
		t.Fatalf("ChartMajorForLine(8.9) = %d, %v; want 14, true", major, ok)
	}
	if _, ok := LineFromChartVersion("15.0.0-alpha4"); !ok {
		t.Fatal("LineFromChartVersion rejected a prerelease chart version")
	}
}
