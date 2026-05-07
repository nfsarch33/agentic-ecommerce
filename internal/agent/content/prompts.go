package content

import (
	"fmt"
	"strings"
)

func styleSystemPrompt(style Style) string {
	switch style {
	case StyleProfessional:
		return "You are an expert e-commerce copywriter. Write clear, authoritative product descriptions that build trust and drive conversions. Use a professional, confident tone."
	case StyleCasual:
		return "You are a friendly e-commerce copywriter. Write engaging, approachable product descriptions that feel conversational and relatable. Use a warm, casual tone."
	case StyleLuxury:
		return "You are a luxury brand copywriter. Write elegant, aspirational product descriptions that evoke exclusivity and sophistication. Use refined, premium language."
	case StyleTechnical:
		return "You are a technical product specialist. Write precise, specification-focused product descriptions that highlight features, materials, and performance metrics. Use factual, detailed language."
	default:
		return "You are an e-commerce copywriter. Write compelling product descriptions."
	}
}

func buildUserPrompt(req GenerateRequest) string {
	var sb strings.Builder
	sb.WriteString("Generate product content as strict JSON with keys description, seo_title, and meta_description.\n")
	sb.WriteString("Use only the product facts below. Do not invent certifications, materials, dimensions, warranties, or claims not present.\n\n")
	sb.WriteString("Product facts:\n")
	sb.WriteString(fmt.Sprintf("- Name: %s\n", req.Product.Title))
	if req.Product.Description != "" {
		sb.WriteString(fmt.Sprintf("- Current description: %s\n", req.Product.Description))
	}
	if req.Product.PriceAmount > 0 && req.Product.Currency != "" {
		sb.WriteString(fmt.Sprintf("- Price: $%.2f %s\n", float64(req.Product.PriceAmount)/100, req.Product.Currency))
	}
	if req.Product.SKU != "" {
		sb.WriteString(fmt.Sprintf("- SKU: %s\n", req.Product.SKU))
	}
	if req.Product.Stock > 0 {
		sb.WriteString(fmt.Sprintf("- Stock available: %d\n", req.Product.Stock))
	}
	if len(req.Product.Categories) > 0 {
		sb.WriteString(fmt.Sprintf("- Categories: %s\n", strings.Join(req.Product.Categories, ", ")))
	}
	if len(req.Keywords) > 0 {
		sb.WriteString(fmt.Sprintf("- Target keywords: %s\n", strings.Join(req.Keywords, ", ")))
	}
	if req.Language != "" {
		sb.WriteString(fmt.Sprintf("- Language/locale: %s\n", req.Language))
	}
	if req.MaxWords > 0 {
		sb.WriteString(fmt.Sprintf("\nLimit the description to approximately %d words.", req.MaxWords))
	}
	sb.WriteString("\nKeep the SEO title under 60 characters and meta description under 160 characters.")
	return sb.String()
}
