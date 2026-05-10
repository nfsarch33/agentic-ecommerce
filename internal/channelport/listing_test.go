package channelport

import "testing"

func TestIsStubChannel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want bool
	}{
		{"instagram", false},
		{"pinterest", false},
		{"INSTAGRAM", false},
		{" pinterest ", false},
		{"tiktok", false},
		{"facebook", false},
		{"rednote", false},
		{"", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := IsStubChannel(c.name); got != c.want {
				t.Fatalf("IsStubChannel(%q)=%v want=%v", c.name, got, c.want)
			}
		})
	}
}

func TestStubChannelNamesEmpty(t *testing.T) {
	t.Parallel()
	if len(StubChannelNames) != 0 {
		t.Fatalf("StubChannelNames should be empty after v4.6.0 promotion, got %d entries", len(StubChannelNames))
	}
}
