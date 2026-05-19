// Package revenueattr provides revenue attribution: multi-touch models, channel scoring, and ROAS.
package revenueattr

import (
	"sort"
	"time"
)

// TouchPoint represents a marketing touch point contributing to a conversion.
type TouchPoint struct {
	Channel    string
	OccurredAt time.Time
	Value      float64
}

// Conversion represents a revenue conversion event with associated touch points.
type Conversion struct {
	ID          string
	Revenue     float64
	TouchPoints []TouchPoint
	OccurredAt  time.Time
}

// Model is the attribution model type.
type Model string

const (
	ModelFirstTouch Model = "first_touch"
	ModelLastTouch  Model = "last_touch"
	ModelLinear     Model = "linear"
	ModelTimeDecay  Model = "time_decay"
)

// AttributeRevenue distributes a conversion's revenue across channels using the given model.
// Returns a map of channel -> attributed revenue. Returns empty map for no touch points.
func AttributeRevenue(conv Conversion, model Model) map[string]float64 {
	result := make(map[string]float64)
	if len(conv.TouchPoints) == 0 {
		return result
	}

	switch model {
	case ModelFirstTouch:
		// All revenue goes to the first touch point (earliest OccurredAt).
		first := earliestTouch(conv.TouchPoints)
		result[first.Channel] += conv.Revenue

	case ModelLastTouch:
		// All revenue goes to the last touch point (latest OccurredAt).
		last := latestTouch(conv.TouchPoints)
		result[last.Channel] += conv.Revenue

	case ModelLinear:
		// Revenue split equally among all touch points.
		share := conv.Revenue / float64(len(conv.TouchPoints))
		for _, tp := range conv.TouchPoints {
			result[tp.Channel] += share
		}

	case ModelTimeDecay:
		// Revenue weighted by proximity to conversion time; more recent = more credit.
		// Weight for each touch = 1 / (1 + hoursBeforeConversion), then normalise.
		weights := make([]float64, len(conv.TouchPoints))
		total := 0.0
		for i, tp := range conv.TouchPoints {
			diff := conv.OccurredAt.Sub(tp.OccurredAt)
			hours := diff.Hours()
			if hours < 0 {
				hours = 0
			}
			w := 1.0 / (1.0 + hours)
			weights[i] = w
			total += w
		}
		for i, tp := range conv.TouchPoints {
			if total > 0 {
				result[tp.Channel] += conv.Revenue * weights[i] / total
			}
		}
	}

	return result
}

// ROAS calculates Return on Ad Spend. Returns 0 if spend is 0.
func ROAS(attributedRevenue, spend float64) float64 {
	if spend == 0 {
		return 0
	}
	return attributedRevenue / spend
}

// ChannelSummary aggregates revenue, spend, and ROAS for a single channel.
type ChannelSummary struct {
	Channel string
	Revenue float64
	Spend   float64
	ROAS    float64
}

// SummaryReport builds a per-channel summary across all conversions with the given model and spend map.
func SummaryReport(conversions []Conversion, model Model, spends map[string]float64) []ChannelSummary {
	revenueByChannel := make(map[string]float64)
	for _, conv := range conversions {
		attr := AttributeRevenue(conv, model)
		for ch, rev := range attr {
			revenueByChannel[ch] += rev
		}
	}

	// Collect all channels from both revenue and spends.
	channelSet := make(map[string]struct{})
	for ch := range revenueByChannel {
		channelSet[ch] = struct{}{}
	}
	for ch := range spends {
		channelSet[ch] = struct{}{}
	}

	summaries := make([]ChannelSummary, 0, len(channelSet))
	for ch := range channelSet {
		rev := revenueByChannel[ch]
		spend := spends[ch]
		summaries = append(summaries, ChannelSummary{
			Channel: ch,
			Revenue: rev,
			Spend:   spend,
			ROAS:    ROAS(rev, spend),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Channel < summaries[j].Channel
	})

	return summaries
}

// earliestTouch returns the touch point with the smallest OccurredAt.
func earliestTouch(tps []TouchPoint) TouchPoint {
	earliest := tps[0]
	for _, tp := range tps[1:] {
		if tp.OccurredAt.Before(earliest.OccurredAt) {
			earliest = tp
		}
	}
	return earliest
}

// latestTouch returns the touch point with the largest OccurredAt.
func latestTouch(tps []TouchPoint) TouchPoint {
	latest := tps[0]
	for _, tp := range tps[1:] {
		if tp.OccurredAt.After(latest.OccurredAt) {
			latest = tp
		}
	}
	return latest
}
