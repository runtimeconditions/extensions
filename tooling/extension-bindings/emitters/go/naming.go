package goemitter

import (
	"fmt"
	"go/token"
	"sort"
	"strings"
)

var initialisms = map[string]bool{
	"ACL": true, "API": true, "ASCII": true, "CPU": true, "CSS": true,
	"DNS": true, "EOF": true, "GUID": true, "HTML": true, "HTTP": true,
	"HTTPS": true, "ID": true, "IP": true, "JSON": true, "QPS": true,
	"RAM": true, "RPC": true, "SDK": true, "SLA": true, "SMTP": true,
	"SQL": true, "SSH": true, "TCP": true, "TLS": true, "TTL": true,
	"UDP": true, "UI": true, "UID": true, "URI": true, "URL": true,
	"UTF8": true, "UUID": true, "VM": true, "XML": true, "XMPP": true,
	"XSRF": true, "XSS": true,
}

func pascal(tokens []string) string {
	var result strings.Builder
	for _, value := range tokens {
		upper := strings.ToUpper(value)
		if initialisms[upper] {
			result.WriteString(upper)
			continue
		}
		lower := strings.ToLower(value)
		if lower == "" {
			continue
		}
		result.WriteString(strings.ToUpper(lower[:1]))
		result.WriteString(lower[1:])
	}
	name := result.String()
	if name == "" {
		return "X"
	}
	if name[0] >= '0' && name[0] <= '9' {
		return "X" + name
	}
	return name
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
