package http

import "strings"

func withAdditionalConnectSrc(csp string, values ...string) string {
	return withAdditionalDirectiveSources(csp, "connect-src", values...)
}

func withAdditionalScriptSrc(csp string, values ...string) string {
	return withAdditionalDirectiveSources(csp, "script-src", values...)
}

func withAdditionalDirectiveSources(csp, directive string, values ...string) string {
	directives := parseCSPDirectives(csp)
	index := indexDirective(directives, directive)
	defaultIndex := indexDirective(directives, "default-src")

	if index >= 0 {
		directives[index].sources = appendUniqueStrings(directives[index].sources, values...)
		return formatCSPDirectives(directives)
	}

	sources := make([]string, 0, len(values)+1)
	if defaultIndex >= 0 {
		sources = append(sources, directives[defaultIndex].sources...)
	}
	sources = appendUniqueStrings(sources, values...)
	directives = append(directives, cspDirective{
		name:    directive,
		sources: sources,
	})
	return formatCSPDirectives(directives)
}

type cspDirective struct {
	name    string
	sources []string
}

func parseCSPDirectives(csp string) []cspDirective {
	parts := strings.Split(csp, ";")
	out := make([]cspDirective, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		out = append(out, cspDirective{
			name:    fields[0],
			sources: append([]string(nil), fields[1:]...),
		})
	}
	return out
}

func formatCSPDirectives(directives []cspDirective) string {
	parts := make([]string, 0, len(directives))
	for _, directive := range directives {
		if directive.name == "" {
			continue
		}
		part := directive.name
		if len(directive.sources) > 0 {
			part += " " + strings.Join(directive.sources, " ")
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

func indexDirective(directives []cspDirective, name string) int {
	for i, directive := range directives {
		if directive.name == name {
			return i
		}
	}
	return -1
}

func appendUniqueStrings(values []string, extras ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(extras))
	out := make([]string, 0, len(values)+len(extras))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, extra := range extras {
		extra = strings.TrimSpace(extra)
		if extra == "" {
			continue
		}
		if _, ok := seen[extra]; ok {
			continue
		}
		seen[extra] = struct{}{}
		out = append(out, extra)
	}
	return out
}
