package flag

import (
	"encoding/json"
	"time"
)

func mustRaw(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic("seed: " + err.Error())
	}
	return encoded
}

func Seed(environment string, now time.Time) Document {
	return Document{
		Environment: environment,
		Version:     1,
		CreatedAt:   now.UTC(),
		CreatedBy:   "seed",
		Comment:     "initial flag set",
		Schema: map[string]string{
			"user.key":       "string",
			"user.country":   "string",
			"user.tier":      "string",
			"user.email":     "string",
			"user.language":  "string",
			"user.age":       "number",
			"user.spend":     "number",
			"user.beta":      "bool",
			"device.type":    "string",
			"device.os":      "string",
			"app.version":    "string",
			"session.region": "string",
		},
		Flags: []Flag{
			{
				Key:         "maintenance-mode",
				Description: "Kill switch for the checkout funnel",
				Kind:        KindBool,
				Enabled:     false,
				Salt:        "maintenance-2026",
				Variations: []Variation{
					{Key: "on", Value: mustRaw(true)},
					{Key: "off", Value: mustRaw(false)},
				},
				Default:      "off",
				OffVariation: "off",
			},
			{
				Key:         "checkout-v2",
				Description: "New checkout, rolling out gradually",
				Kind:        KindBool,
				Enabled:     true,
				Salt:        "checkout-v2-2026",
				Variations: []Variation{
					{Key: "on", Value: mustRaw(true)},
					{Key: "off", Value: mustRaw(false)},
				},
				Default:      "off",
				OffVariation: "off",
				Rules: []Rule{
					{
						ID:          "internal-testers",
						Description: "Anyone opted into beta sees it immediately",
						Expression:  `user.beta`,
						Serve:       Serve{Variation: "on"},
					},
					{
						ID:          "vip-dach",
						Description: "High spenders in German-speaking markets",
						Expression:  `user.country in ["DE", "AT", "CH"] and user.tier == "vip" and user.spend > 250`,
						Serve:       Serve{Variation: "on"},
					},
					{
						ID:          "gradual-eu",
						Description: "Ten percent of the rest of the EU",
						Expression:  `user.country in ["DE", "AT", "CH", "PL", "NL", "FI", "SE", "NO", "DK"]`,
						Serve: Serve{Rollout: []RolloutEntry{
							{Variation: "on", Weight: 10},
							{Variation: "off", Weight: 90},
						}},
					},
				},
			},
			{
				Key:         "pricing-banner-copy",
				Description: "Three-way copy experiment on the pricing page",
				Kind:        KindString,
				Enabled:     true,
				Salt:        "pricing-copy-2026",
				Variations: []Variation{
					{Key: "control", Value: mustRaw("Start your free trial")},
					{Key: "urgency", Value: mustRaw("Start free — 2 minutes to set up")},
					{Key: "value", Value: mustRaw("Everything included, cancel anytime")},
				},
				Default:      "control",
				OffVariation: "control",
				Rules: []Rule{
					{
						ID:          "english-speakers",
						Description: "Even three-way split for English copy only",
						Expression:  `user.language == "en"`,
						Serve: Serve{Rollout: []RolloutEntry{
							{Variation: "control", Weight: 34},
							{Variation: "urgency", Weight: 33},
							{Variation: "value", Weight: 33},
						}},
					},
				},
			},
			{
				Key:         "upload-size-limit-mb",
				Description: "Per-tier upload ceiling",
				Kind:        KindNumber,
				Enabled:     true,
				Salt:        "upload-limit-2026",
				Variations: []Variation{
					{Key: "free", Value: mustRaw(25)},
					{Key: "pro", Value: mustRaw(250)},
					{Key: "enterprise", Value: mustRaw(2048)},
				},
				Default:      "free",
				OffVariation: "free",
				Rules: []Rule{
					{
						ID:         "enterprise-tier",
						Expression: `user.tier == "enterprise"`,
						Serve:      Serve{Variation: "enterprise"},
					},
					{
						ID:         "paying-tiers",
						Expression: `user.tier in ["pro", "vip"]`,
						Serve:      Serve{Variation: "pro"},
					},
				},
			},
			{
				Key:         "legacy-ios-warning",
				Description: "Warn users still on an old build",
				Kind:        KindBool,
				Enabled:     true,
				Salt:        "legacy-ios-2026",
				Variations: []Variation{
					{Key: "show", Value: mustRaw(true)},
					{Key: "hide", Value: mustRaw(false)},
				},
				Default:      "hide",
				OffVariation: "hide",
				Rules: []Rule{
					{
						ID:         "old-builds",
						Expression: `device.os == "ios" and app.version matches "^[0-3]\\."`,
						Serve:      Serve{Variation: "show"},
					},
				},
			},
		},
	}
}
