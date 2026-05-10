package channelport

import "testing"

func TestIsStubChannel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want bool
	}{
		{"instagram", true},
		{"pinterest", true},
		{"INSTAGRAM", true},
		{" pinterest ", true},
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

func TestStubChannelNamesPopulated(t *testing.T) {
	t.Parallel()
	if _, ok := StubChannelNames["instagram"]; !ok {
		t.Fatal("instagram missing from StubChannelNames")
	}
	if _, ok := StubChannelNames["pinterest"]; !ok {
		t.Fatal("pinterest missing from StubChannelNames")
	}
}
