// Package agentmodels lee los modelos que cada agente tiene configurados, para
// que elegir uno sea escoger de una lista y no recordar cómo se escribe.
// Reasonix los declara por proveedor en su TOML; Claude Code guarda los suyos
// en el JSON de configuración del usuario.
package agentmodels

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Model es una opción del selector.
type Model struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

// reasonixShape es la parte del TOML que interesa. Se lee aparte de la carga
// normal de configuración porque aquí solo hace falta la lista, sin heredar
// overrides de proyecto ni validaciones.
type reasonixShape struct {
	DefaultModel string `toml:"default_model"`
	Providers    []struct {
		Name    string   `toml:"name"`
		Model   string   `toml:"model"`
		Models  []string `toml:"models"`
		Default string   `toml:"default"`
	} `toml:"providers"`
}

// Reasonix lista los modelos como "proveedor/modelo", que es la forma que
// espera la bandera --model.
func Reasonix(configPath string) ([]Model, error) {
	var shape reasonixShape
	if _, err := toml.DecodeFile(configPath, &shape); err != nil {
		return nil, err
	}
	var out []Model
	seen := map[string]bool{}
	for _, p := range shape.Providers {
		names := p.Models
		if p.Model != "" {
			names = append(names, p.Model)
		}
		for _, m := range names {
			value := p.Name + "/" + m
			if m == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, Model{
				Value:       value,
				Label:       m,
				Description: p.Name,
				Default:     value == shape.DefaultModel,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return out[i].Description < out[j].Description
	})
	return out, nil
}

// claudeAliases son los nombres cortos que el binario acepta siempre. Van
// primero porque apuntan al modelo más reciente de cada familia y no caducan
// cuando sale una versión nueva.
var claudeAliases = []Model{
	{Value: "fable", Label: "fable", Description: "alias del Fable más reciente"},
	{Value: "opus", Label: "opus", Description: "alias del Opus más reciente"},
	{Value: "sonnet", Label: "sonnet", Description: "alias del Sonnet más reciente"},
	{Value: "haiku", Label: "haiku", Description: "alias del Haiku más reciente"},
}

// Claude lista los alias estables más lo que el picker haya cacheado en el
// JSON del usuario, que es de donde salen los nombres completos con sufijo.
func Claude(configPath string) ([]Model, error) {
	out := append([]Model(nil), claudeAliases...)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return out, err
	}
	var shape struct {
		Options []struct {
			Value       string `json:"value"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"additionalModelOptionsCache"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return out, err
	}
	seen := map[string]bool{}
	for _, m := range out {
		seen[m.Value] = true
	}
	for _, o := range shape.Options {
		value := strings.TrimSpace(o.Value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		label := o.Label
		if label == "" {
			label = value
		}
		out = append(out, Model{Value: value, Label: label, Description: o.Description})
	}
	return out, nil
}
