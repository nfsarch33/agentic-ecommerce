// Package contentfmt validates social post payloads against the per-platform
// format limits the content engine must honour before anything is queued for
// publishing (v18870-3). The numbers live in the contract test until the
// implementation lands; this stub exists so the contract compiles and fails.
package contentfmt

// Platform identifies a publishing destination.
type Platform string

const (
	Instagram Platform = "instagram"
	X         Platform = "x"
	YouTube   Platform = "youtube"
	LinkedIn  Platform = "linkedin"
)

// Media is one slide or attachment. Format is the container/extension of the
// asset as the publisher will upload it (for example "jpg", "png", "mp4").
type Media struct {
	Format string
}

// Post is the subset of a content draft the format rules can judge.
type Post struct {
	Platform    Platform
	Caption     string
	Hashtags    []string
	Media       []Media
	Title       string
	Description string
}

// Violation is one broken rule. Rule names are stable identifiers, not prose.
type Violation struct {
	Rule   string
	Detail string
}

// Validate is the contract under construction: one Violation per broken
// rule, stable order, empty when format-clean. The stub fails every case.
func Validate(Post) []Violation { return nil }
