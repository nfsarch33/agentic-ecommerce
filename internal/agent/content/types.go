package content

// Style determines the tone of generated commerce copy.
type Style string

const (
	StyleProfessional Style = "professional"
	StyleCasual       Style = "casual"
	StyleLuxury       Style = "luxury"
	StyleTechnical    Style = "technical"
)

// ProductInfo is the product subset needed by the content agent.
type ProductInfo struct {
	ID          string
	SKU         string
	Title       string
	Description string
	PriceAmount int
	Currency    string
	Stock       int
	Categories  []string
}

// GenerateRequest asks the content agent for product marketing copy.
type GenerateRequest struct {
	Product  ProductInfo
	Style    Style
	Language string
	MaxWords int
	Keywords []string
}

// Plan describes the prompt strategy before provider execution.
type Plan struct {
	Goal        string
	System      string
	User        string
	Request     GenerateRequest
	MaxTokens   int
	Temperature float64
}

// GeneratedContent is the structured marketing copy generated for a product.
type GeneratedContent struct {
	Description     string `json:"description"`
	SEOTitle        string `json:"seo_title"`
	MetaDescription string `json:"meta_description"`
}

// GenerateResult combines generated copy with quality evaluation.
type GenerateResult struct {
	GeneratedContent
	Request    GenerateRequest `json:"-"`
	Evaluation Evaluation      `json:"evaluation"`
	TokensUsed int             `json:"tokens_used"`
}

// Report summarizes an agent run for API responses and future run history.
type Report struct {
	ProductID  string `json:"product_id"`
	Score      int    `json:"score"`
	Pass       bool   `json:"pass"`
	TokensUsed int    `json:"tokens_used"`
}

// EvaluationInput describes content quality checks.
type EvaluationInput struct {
	Product  ProductInfo
	Output   GeneratedContent
	Style    Style
	MaxWords int
	Keywords []string
}

type Evaluation struct {
	Score            int                `json:"score"`
	Pass             bool               `json:"pass"`
	ReadabilityScore float64            `json:"readability_score"`
	KeywordDensity   map[string]float64 `json:"keyword_density"`
	Tone             ToneEvaluation     `json:"tone"`
	Length           LengthEvaluation   `json:"length"`
	FactualIssues    []string           `json:"factual_issues"`
}

type ToneEvaluation struct {
	Style  Style    `json:"style"`
	Pass   bool     `json:"pass"`
	Issues []string `json:"issues"`
}

type LengthEvaluation struct {
	WordCount   int  `json:"word_count"`
	MaxWords    int  `json:"max_words"`
	WithinLimit bool `json:"within_limit"`
}
