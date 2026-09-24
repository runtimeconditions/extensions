package goemitter

import (
	"fmt"
	"go/token"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// goTokens derives Go naming components from the exact source name. The
// normalized model intentionally does not carry language-specific tokens.
func goTokens(value string) []string {
	var result []string
	var current []rune
	runes := []rune(value)
	flush := func() {
		if len(current) != 0 {
			result = append(result, string(current))
			current = nil
		}
	}
	for index, character := range runes {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			flush()
			continue
		}
		if len(current) != 0 {
			previous := runes[index-1]
			next := rune(0)
			if index+1 < len(runes) {
				next = runes[index+1]
			}
			boundary := unicode.IsUpper(character) && (unicode.IsLower(previous) || unicode.IsDigit(previous))
			boundary = boundary || (unicode.IsUpper(character) && unicode.IsUpper(previous) && unicode.IsLower(next))
			boundary = boundary || (unicode.IsDigit(character) != unicode.IsDigit(previous))
			if boundary {
				flush()
			}
		}
		current = append(current, character)
	}
	flush()
	return result
}

func pascal(tokens []string) string {
	var result strings.Builder
	for _, value := range tokens {
		lower := strings.ToLower(value)
		if lower == "" {
			continue
		}
		first, size := utf8.DecodeRuneInString(lower)
		result.WriteString(strings.ToUpper(string(first)))
		result.WriteString(lower[size:])
	}
	name := result.String()
	return name
}

func validateGoIdentifier(name string, request *symbolRequest) error {
	if name != "" && token.IsIdentifier(name) && !token.Lookup(name).IsKeyword() {
		return nil
	}
	return &DiagnosticError{Diagnostic: Diagnostic{
		Category:   "symbol",
		Code:       "RCG2011",
		Coordinate: request.coordinate,
		Message:    fmt.Sprintf("Go source name for %s %q produces invalid identifier %q", request.category, request.coordinate, name),
	}}
}

func lowerIdentifier(tokens []string) string {
	var result strings.Builder
	for _, value := range tokens {
		result.WriteString(strings.ToLower(value))
	}
	name := result.String()
	if name == "" {
		name = "binding"
	}
	if token.Lookup(name).IsKeyword() {
		name += "binding"
	}
	return name
}

type symbolRequest struct {
	key          string
	coordinate   string
	category     string
	base         []string
	prefixGroups [][]string
	fixed        bool
	name         string
	level        int
}

func allocateSymbols(packageCoordinate string, requests []*symbolRequest) error {
	for _, request := range requests {
		request.name = pascal(request.base)
		if err := validateGoIdentifier(request.name, request); err != nil {
			return err
		}
	}
	for {
		groups := map[string][]*symbolRequest{}
		for _, request := range requests {
			groups[request.name] = append(groups[request.name], request)
		}
		names := make([]string, 0, len(groups))
		for name := range groups {
			names = append(names, name)
		}
		sort.Strings(names)
		changed := false
		for _, name := range names {
			group := groups[name]
			if len(group) < 2 {
				continue
			}
			for _, request := range group {
				if request.fixed {
					return symbolCollision(packageCoordinate, name, group[0], group[1])
				}
			}
			for _, request := range group {
				if request.level >= len(request.prefixGroups) {
					return symbolCollision(packageCoordinate, name, group[0], group[1])
				}
				request.level++
				var tokens []string
				for index := request.level - 1; index >= 0; index-- {
					tokens = append(tokens, request.prefixGroups[index]...)
				}
				tokens = append(tokens, request.base...)
				request.name = pascal(tokens)
				if err := validateGoIdentifier(request.name, request); err != nil {
					return err
				}
			}
			changed = true
		}
		if !changed {
			return nil
		}
	}
}

func allocatedName(request *symbolRequest) string {
	var tokens []string
	for index := request.level - 1; index >= 0; index-- {
		tokens = append(tokens, request.prefixGroups[index]...)
	}
	tokens = append(tokens, request.base...)
	return pascal(tokens)
}

func symbolCollision(packageCoordinate, name string, first, second *symbolRequest) error {
	return &DiagnosticError{Diagnostic: Diagnostic{
		Category:   "symbol",
		Code:       "RCG2001",
		Coordinate: packageCoordinate,
		Message: fmt.Sprintf(
			"Go symbol %q collides between %s %q and %s %q in package %q",
			name, first.category, first.coordinate, second.category, second.coordinate, packageCoordinate,
		),
	}}
}
