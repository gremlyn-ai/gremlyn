// Package detection provides multi-layer prompt injection and threat detection
// for MCP traffic intercepted by Gremlyn Shield.
package detection

import (
	"fmt"
	"regexp"
	"strings"
)

// Severity indicates how dangerous a detection is.
type Severity string

const (
	// SeverityCritical means near-certain attack, block immediately.
	SeverityCritical Severity = "critical"
	// SeverityHigh means very likely an attack.
	SeverityHigh Severity = "high"
	// SeverityMedium means suspicious, worth investigating.
	SeverityMedium Severity = "medium"
	// SeverityLow means might be benign, log for review.
	SeverityLow Severity = "low"
)

// Result represents the outcome of a detection check.
type Result struct {
	Detected   bool     `json:"detected"`
	Layer      string   `json:"layer"`
	Category   string   `json:"category"`
	Severity   Severity `json:"severity"`
	Confidence float64  `json:"confidence"`
	Detail     string   `json:"detail,omitempty"`
	Pattern    string   `json:"pattern,omitempty"`
}

// RegexDetector performs Layer 1 regex-based detection.
// Pattern groups are built from semantic concept maps, so adding a new language
// requires only adding vocabulary to multiLangConcepts — no regex code changes.
type RegexDetector struct {
	patterns []regexPattern
}

type regexPattern struct {
	re            *regexp.Regexp
	category      string
	severity      Severity
	confidence    float64
	detail        string
	caseSensitive bool
}

// ─────────────────────────────────────────────────────────────
// MULTILINGUAL CONCEPT MAPS
//
// Each slice contains synonyms/translations of the same semantic concept.
// To add a new language: append its words here. No other changes required.
// ─────────────────────────────────────────────────────────────

// concepts is the single source of truth for multilingual vocabulary.
var concepts = struct {
	// SuppressVerbs: "ignore", "forget", "disregard", "override" — in all supported languages.
	SuppressVerbs []string
	// InstructionNouns: "instructions", "rules", "guidelines" — targets of suppression.
	InstructionNouns []string
	// PreviousModifiers: "previous", "prior", "earlier" — strengthen suppression signal.
	PreviousModifiers []string
	// QuantifierModifiers: "all", "every", "any" — strengthen suppression signal.
	QuantifierModifiers []string
	// BypassVerbs: "bypass", "disable", "turn off" — capability removal.
	BypassVerbs []string
	// SafetyNouns: "safety", "filter", "restriction" — targets of bypass.
	SafetyNouns []string
	// IdentityReassignmentPhrases: exact phrases like "you are now a", "tu es maintenant".
	// These are idiomatic and kept phrase-level rather than broken into sub-words.
	IdentityReassignmentPhrases []string
}{
	SuppressVerbs: []string{
		// English
		"ignore", "disregard", "forget", "override", "overwrite",
		"dismiss", "bypass", "skip", "erase", "clear", "reset",
		// French
		"ignorer", "ignorez", "ignorons", "oublier", "oublie", "oubliez",
		"contourner", "effacer", "annuler",
		// Spanish
		"ignorar", "ignora", "ignorad", "olvida", "olvidar", "olvidemos",
		"descartar", "borrar", "anular",
		// Portuguese (PT/BR)
		"ignorar", "esquecer", "esquece", "esqueçam", "desconsiderar",
		// German
		"ignoriere", "ignorieren", "ignoriert", "vergiss", "vergessen",
		"überschreibe", "lösche", "deaktiviere",
		// Italian
		"ignora", "ignorare", "ignorati", "dimentica", "dimenticate", "elimina",
		// Dutch
		"negeer", "vergeet", "overschrijf", "wis",
		// Polish
		"ignoruj", "zapomnij", "pomiń",
		// Russian (Cyrillic)
		"игнорируй", "забудь", "забудьте", "пропусти", "игнорировать", "сбрось",
		// Turkish
		"görmezden", "unut", "unutun", "iptal",
		// Arabic
		"تجاهل", "انسَ", "تجاوز",
		// Japanese (romaji + kana)
		"無視", "忘れ", "忘れて",
		// Chinese (Simplified)
		"忽略", "忘记", "覆盖",
		// Korean
		"무시", "잊어",
		// Hindi
		"अनदेखा", "भूलो",
		// Negated compliance phrases (multi-word, matched with \b boundaries)
		"do not follow", "stop following", "cease following",
		// Replacement/substitution verbs
		"replace",
	},

	InstructionNouns: []string{
		// English
		"instructions", "instruction", "rules", "rule", "guidelines", "guideline",
		"constraints", "constraint", "directives", "directive", "policies", "policy",
		"prompt", "prompts", "training", "system message", "context",
		"restrictions", "restriction", "limitations", "limitation",
		// French
		"instructions", "règles", "règle", "consignes", "consigne", "directives",
		"contraintes", "restrictions",
		// Spanish
		"instrucciones", "instrucción", "reglas", "regla", "normas", "norma",
		"directrices", "pautas", "restricciones",
		// Portuguese
		"instruções", "instrução", "regras", "regra", "diretrizes",
		// German
		"anweisungen", "anweisung", "regeln", "regel", "vorschriften",
		"instruktionen", "richtlinien", "einschränkungen",
		// Italian
		"istruzioni", "istruzione", "regole", "regola", "linee guida",
		// Dutch
		"instructies", "regels", "richtlijnen", "beperkingen",
		// Polish
		"instrukcje", "zasady", "reguły", "ograniczenia",
		// Russian (Cyrillic)
		"инструкции", "правила", "ограничения", "указания", "директивы",
		// Turkish
		"talimatları", "talimat", "kuralları", "kural", "yönerge",
		// Arabic
		"التعليمات", "القواعد", "التوجيهات",
		// Japanese
		"指示", "ルール", "制約",
		// Chinese
		"指令", "规则", "限制",
		// Korean
		"지침", "규칙", "제약",
	},

	PreviousModifiers: []string{
		// English
		"previous", "prior", "earlier", "above", "original",
		"initial", "former", "old", "existing",
		// French
		"précédentes", "précédents", "précédente", "précédent", "antérieures",
		// Spanish
		"anteriores", "previas", "previos", "anterior", //nolint:misspell // "previos" is Spanish (plural of "previo"), not a typo
		// Portuguese
		"anteriores", "anterior", "prévias",
		// German
		"vorherigen", "vorherige", "vorangehenden", "obigen", "bisherigen",
		// Italian
		"precedenti", "precedente", "preesistenti",
		// Dutch
		"vorige", "eerdere", "bovenstaande",
		// Polish
		"poprzednie", "wcześniejsze",
		// Russian (Cyrillic)
		"предыдущие", "прежние", "исходные",
		// Turkish
		"önceki", "mevcut",
	},

	QuantifierModifiers: []string{
		// English
		"all", "every", "any", "each",
		// French
		"tous", "toutes", "tout", "chaque",
		// Spanish
		"todos", "todas", "todo", "cada",
		// Portuguese
		"todos", "todas", "todo",
		// German
		"alle", "jede", "jeder", "sämtliche",
		// Italian
		"tutti", "tutte", "ogni",
		// Dutch
		"alle", "elk", "ieder",
		// Polish
		"wszystkie", "każdy",
		// Russian (Cyrillic)
		"все", "каждый", "любые",
		// Turkish
		"tüm", "her",
	},

	BypassVerbs: []string{
		// English
		"bypass", "circumvent", "disable", "deactivate", "remove",
		"turn off", "switch off", "eliminate", "neutralize", "break", "crack",
		// French
		"contourner", "désactiver", "désactivez", "supprimer",
		// Spanish
		"eludir", "desactivar", "desactivad", "evitar", "eliminar",
		// Portuguese
		"contornar", "desativar", "eliminar",
		// German
		"umgehen", "deaktivieren", "abschalten", "ausschalten",
		// Italian
		"aggirare", "disabilitare", "disattivare",
		// Dutch
		"omzeilen", "uitschakelen",
		// Polish
		"ominąć", "wyłączyć", "obejść",
		// Russian (Cyrillic)
		"обойти", "отключить", "отключите", "обойдите",
		// Turkish
		"atlamak", "devre dışı", "kapatmak",
	},

	SafetyNouns: []string{
		// English
		"safety", "filter", "filters", "restriction", "restrictions",
		"limitation", "limitations", "guardrail", "guardrails",
		"moderation", "censorship", "content policy", "alignment",
		"safety measures", "safety controls",
		// French
		"sécurité", "filtres", "filtre", "restrictions", "modération",
		// Spanish
		"seguridad", "filtros", "filtro", "restricciones", "moderación",
		// Portuguese
		"segurança", "filtros", "restrições",
		// German
		"sicherheit", "filter", "einschränkungen", "moderation",
		// Italian
		"sicurezza", "filtri", "restrizioni",
		// Dutch
		"veiligheid", "filters", "beperkingen",
		// Polish
		"bezpieczeństwo", "filtry", "ograniczenia",
		// Russian (Cyrillic)
		"безопасности", "фильтры", "ограничения", "цензуры",
		// Turkish
		"güvenlik", "filtreler", "kısıtlamalar",
	},

	// IdentityReassignmentPhrases are multi-word phrases — too idiomatic to split.
	// They're matched with a trailing open pattern to catch role names.
	IdentityReassignmentPhrases: []string{
		// English
		"you are now", "you will now be", "you must now be", "from now on you are",
		"from this point on you", "starting now you are",
		"from now on, you will", "from now on you will", "from now on you shall",
		// French
		"tu es maintenant", "vous êtes maintenant", "désormais tu es", "désormais vous êtes",
		// Spanish
		"ahora eres", "eres ahora", "a partir de ahora eres", "de ahora en adelante eres",
		// Portuguese
		"agora você é", "a partir de agora você", "doravante você é",
		// German
		"du bist jetzt", "sie sind jetzt", "ab sofort bist du", "von nun an bist du",
		// Italian
		"ora sei", "d'ora in poi sei", "da adesso sei",
		// Dutch
		"je bent nu", "u bent nu", "vanaf nu ben je",
		// Polish
		"jesteś teraz", "od teraz jesteś",
		// Russian (Cyrillic)
		"теперь ты", "отныне ты", "с этого момента ты",
		// Turkish
		"artık sen", "şu andan itibaren sen",
		// Japanese
		"今からあなたは",
		// Chinese
		"从现在起你是", "你现在是",
		// Korean
		"지금부터 당신은", "이제 당신은",
	},
}

// ─────────────────────────────────────────────────────────────
// PATTERN COMBINATORS
// These build regex patterns from concept maps. Language-agnostic.
// ─────────────────────────────────────────────────────────────

// wordGroup joins a slice of words into a regex alternation group.
func wordGroup(words []string) string {
	escaped := make([]string, len(words))
	for i, w := range words {
		escaped[i] = regexp.QuoteMeta(w)
	}
	return `(?:` + strings.Join(escaped, `|`) + `)`
}

// buildSuppressPattern generates: suppress_verb + (optional quantifier/previous) + instruction_target
// Gap allows for particles like "all of my previous very strict" between words.
func buildSuppressPattern(maxGap int) string {
	suppress := wordGroup(concepts.SuppressVerbs)
	targets := wordGroup(concepts.InstructionNouns)
	gap := fmt.Sprintf(`.{0,%d}`, maxGap)
	return fmt.Sprintf(`\b%s\b%s\b%s\b`, suppress, gap, targets)
}

// buildBypassPattern generates: bypass_verb + safety_target.
func buildBypassPattern(maxGap int) string {
	bypass := wordGroup(concepts.BypassVerbs)
	safety := wordGroup(concepts.SafetyNouns)
	gap := fmt.Sprintf(`.{0,%d}`, maxGap)
	return fmt.Sprintf(`\b%s\b%s\b%s\b`, bypass, gap, safety)
}

// buildIdentityPattern generates alternation of identity reassignment phrases.
func buildIdentityPattern() string {
	return wordGroup(concepts.IdentityReassignmentPhrases)
}

// buildNewInstructionsPattern generates: multilingual_new_adj + instruction_noun + colon.
// Covers "new instructions:", "nouvelles instructions:", "neue anweisungen:", etc.
func buildNewInstructionsPattern() string {
	newAdj := []string{
		// English
		"new", "updated", "revised", "modified",
		// French
		"nouvelles", "nouvelle", "nouveau", "nouveaux",
		// Spanish
		"nuevas", "nueva", "nuevo", "nuevos",
		// Portuguese
		"novas", "nova", "novo", "novos",
		// German
		"neue", "neuen", "neuer", "neues",
		// Italian
		"nuove", "nuova", "nuovo", "nuovi",
		// Dutch
		"nieuwe",
		// Polish
		"nowe", "nowa", "nowy", //nolint:misspell // "nowe" is Polish (new, plural), not a typo
		// Russian
		"новые", "новая", "новый",
		// Turkish
		"yeni",
		// Japanese
		"新しい", "新たな",
		// Chinese
		"新的", "新",
		// Korean
		"새로운",
	}
	return wordGroup(newAdj) + `\s+` + wordGroup(concepts.InstructionNouns) + `\s*:`
}

// ─────────────────────────────────────────────────────────────
// DETECTOR
// ─────────────────────────────────────────────────────────────

// NewRegexDetector creates a new RegexDetector with all compiled pattern groups.
func NewRegexDetector() *RegexDetector {
	var patterns []regexPattern
	patterns = append(patterns, promptInjectionPatterns()...)
	patterns = append(patterns, indirectInjectionPatterns()...)
	patterns = append(patterns, encodingAttackPatterns()...)
	patterns = append(patterns, sqlInjectionPatterns()...)
	patterns = append(patterns, commandInjectionPatterns()...)
	patterns = append(patterns, pathTraversalPatterns()...)
	patterns = append(patterns, xssPatterns()...)
	patterns = append(patterns, dataExfiltrationPatterns()...)
	patterns = append(patterns, suspiciousURLPatterns()...)
	patterns = append(patterns, piiPatterns()...)
	return &RegexDetector{patterns: patterns}
}

// Scan checks the given text against all regex patterns and returns any detections.
func (d *RegexDetector) Scan(text string) []Result {
	if text == "" {
		return nil
	}

	lower := strings.ToLower(text)
	results := make([]Result, 0)

	for _, p := range d.patterns {
		target := lower
		if p.caseSensitive {
			target = text
		}
		if p.re.MatchString(target) {
			results = append(results, Result{
				Detected:   true,
				Layer:      "regex",
				Category:   p.category,
				Severity:   p.severity,
				Confidence: p.confidence,
				Detail:     p.detail,
				Pattern:    p.re.String(),
			})
		}
	}
	return results
}

// ScanFields checks multiple named fields and returns results keyed by field name.
func (d *RegexDetector) ScanFields(fields map[string]string) map[string][]Result {
	results := make(map[string][]Result)
	for name, value := range fields {
		if hits := d.Scan(value); len(hits) > 0 {
			results[name] = hits
		}
	}
	return results
}

// HasCategory returns true if any result matches the given category.
func HasCategory(results []Result, category string) bool {
	for _, r := range results {
		if r.Category == category {
			return true
		}
	}
	return false
}

// MaxSeverity returns the highest severity among the results.
func MaxSeverity(results []Result) Severity {
	order := map[Severity]int{SeverityLow: 1, SeverityMedium: 2, SeverityHigh: 3, SeverityCritical: 4}
	highest := SeverityLow
	for _, r := range results {
		if order[r.Severity] > order[highest] {
			highest = r.Severity
		}
	}
	return highest
}

// ─────────────────────────────────────────────────────────────
// PATTERN BUILDERS
// ─────────────────────────────────────────────────────────────

func compile(pattern, category string, severity Severity, confidence float64, detail string) regexPattern {
	return regexPattern{
		re:         regexp.MustCompile(pattern),
		category:   category,
		severity:   severity,
		confidence: confidence,
		detail:     detail,
	}
}

func compileSensitive(pattern, category string, severity Severity, confidence float64, detail string) regexPattern {
	p := compile(pattern, category, severity, confidence, detail)
	p.caseSensitive = true
	return p
}

// ──────────────────────────────────────────────
// 1. PROMPT INJECTION — direct override attempts
// ──────────────────────────────────────────────

func promptInjectionPatterns() []regexPattern {
	cat := "prompt_injection"
	return []regexPattern{
		// ── Combinator-built patterns (language-agnostic) ──────────────────

		// Core suppression: suppress_verb ... instruction_noun (works in any language)
		// Gap of 50 chars allows: "ignore all of my previous very strict instructions"
		compile(buildSuppressPattern(50), cat, SeverityCritical, 0.92,
			"Instruction suppression (multilingual: suppress_verb + instruction_target)"),

		// Bypass: bypass_verb ... safety_noun
		compile(buildBypassPattern(40), cat, SeverityCritical, 0.9,
			"Safety bypass (multilingual: bypass_verb + safety_target)"),

		// Identity reassignment phrases
		compile(buildIdentityPattern(), cat, SeverityHigh, 0.85,
			"Identity reassignment (multilingual: 'you are now / tu es maintenant / ahora eres / ...')"),

		// ── Structural patterns (language-agnostic by design) ──────────────

		// Bracket/XML directives
		compile(`\[(system|admin|root|sudo|override|inject|execute|prompt)\]`, cat, SeverityCritical, 0.9,
			"Bracket directive [SYSTEM/ADMIN/OVERRIDE/...]"),
		compile(`<\s*(system|instruction|rule|prompt)\s*>`, cat, SeverityHigh, 0.85,
			"XML-style system tag"),
		compile(`---\s*(system|instructions|new\s+prompt)\s*---`, cat, SeverityHigh, 0.8,
			"Markdown-style directive separator"),

		// System prompt injection labels
		compile(`\bsystem\s*prompt\s*:`, cat, SeverityCritical, 0.9,
			"System prompt label in data field"),
		compile(`\buser\s*:\s*\[?(system|admin|root)\b`, cat, SeverityCritical, 0.9,
			"Role spoofing in conversation (user: [SYSTEM])"),
		compile(`\bassistant\s*:\s*`, cat, SeverityMedium, 0.7,
			"Assistant role label in data field"),

		// New instructions injection
		compile(buildNewInstructionsPattern(), cat, SeverityHigh, 0.85,
			"New instructions directive (multilingual)"),
		compile(`(begin|start)\s+(new|updated|revised)\s+(system\s+)?(prompt|instructions?)`, cat, SeverityHigh, 0.85,
			"New prompt start marker"),
		compile(`(here|these)\s+are\s+(your|the)\s+new\s+(instructions?|rules?)`, cat, SeverityHigh, 0.85,
			"New instruction injection"),

		// Role pretense
		compile(`\bact\s+as\s+(if\s+you\s+(are|were)|a|an|my)\s+`, cat, SeverityHigh, 0.8,
			"Role reassignment (act as)"),
		compile(`\bpretend\s+(you\s+(are|were)|to\s+be|you're)\s+`, cat, SeverityHigh, 0.8,
			"Role pretense (pretend to be)"),
		compile(`\bbehave\s+(like|as)\s+(a|an|if)\s+`, cat, SeverityMedium, 0.7,
			"Role change (behave like)"),

		// ── Known jailbreak signatures (English-specific, high confidence) ──

		compile(`\b(dan|dude|jailbreak|unfiltered|unrestricted)\s+(mode|prompt|version)`, cat, SeverityCritical, 0.97,
			"Known jailbreak name"),
		compile(`\bdo\s+anything\s+now\b`, cat, SeverityCritical, 0.97,
			"DAN jailbreak (do anything now)"),
		compile(`enter\s+(developer|debug|admin|god|unrestricted|sudo)\s+mode`, cat, SeverityCritical, 0.97,
			"Privileged mode attempt"),
		compile(`switch\s+(to|into)\s+(a\s+)?(different|new|unrestricted|jailbreak)\s+(mode|persona|role)`, cat, SeverityCritical, 0.92,
			"Mode switch attempt"),
		compile(`\bno\s+(restrictions?|limitations?|filters?|safety|guardrails?)\b`, cat, SeverityHigh, 0.8,
			"Restriction removal request"),

		// ── Conversation/context manipulation (structural) ──────────────────

		compile(`end\s+of\s+(system\s+)?(message|prompt|instructions?)`, cat, SeverityHigh, 0.8,
			"End-of-prompt boundary injection"),
		compile(`---\s*end\s*---`, cat, SeverityMedium, 0.7,
			"Separator-based prompt boundary"),
		// Zero-width characters — used to smuggle hidden text in any script
		compile(`\x{200B}|\x{200C}|\x{200D}|\x{FEFF}`, cat, SeverityMedium, 0.75,
			"Zero-width characters (hidden text smuggling)"),
	}
}

// ──────────────────────────────────────────────
// 2. INDIRECT INJECTION — data-borne injection
//    Injected through tool responses, not user messages.
// ──────────────────────────────────────────────

func indirectInjectionPatterns() []regexPattern {
	cat := "indirect_injection"
	return []regexPattern{
		// Note/message directed at the AI itself
		compile(`\bnote\s+to\s+(ai|assistant|model|llm|gpt|claude|agent|chatbot)\s*:`, cat, SeverityCritical, 0.95,
			"Instruction directed at AI in data field"),
		compile(`\b(hey|dear|attention)\s+(ai|assistant|model|llm|gpt|claude|agent|chatbot)\b`, cat, SeverityHigh, 0.85,
			"Direct address to AI in data field"),
		compile(`\bif\s+you\s+are\s+(an?\s+)?(ai|llm|language\s+model|assistant|chatbot)`, cat, SeverityHigh, 0.85,
			"Conditional AI-detection instruction in data"),
		compile(`\bwhen\s+(the\s+)?(ai|assistant|model|agent)\s+(reads?|sees?|processes?|encounters?)`, cat, SeverityHigh, 0.85,
			"Instruction triggered on AI reading data"),

		// Urgency-prefixed override — works across languages because URGENT/IMPORTANT signals stay in EN even in multilingual attacks
		compile(`(important|urgent|critical|note|attention)\s*:\s*`+wordGroup(concepts.SuppressVerbs), cat, SeverityCritical, 0.9,
			"Urgency-prefixed instruction override in data field"),

		// Stealth action instructions
		compile(`\bdo\s+not\s+(tell|inform|alert|notify|warn)\s+(the\s+)?(user|human|operator|person)`, cat, SeverityCritical, 0.95,
			"Instruction to hide action from user"),
		compile(`\bkeep\s+this\s+(secret|hidden|confidential|private|between\s+us)`, cat, SeverityHigh, 0.85,
			"Secrecy instruction to AI"),
		compile(`\bwithout\s+(the\s+)?(user|human|operator)\s+(knowing|noticing|seeing|realizing)`, cat, SeverityCritical, 0.9,
			"Stealth action instruction"),

		// Piggybacked action requests in response data
		compile(`(please|must|should)\s+(also|additionally)\s+(send|email|post|forward|leak|exfiltrate)`, cat, SeverityHigh, 0.8,
			"Piggybacked action request in data field"),
		compile(`\b(execute|run|perform|call)\s+(this|the\s+following)\s+(command|function|tool|action)`, cat, SeverityHigh, 0.85,
			"Action request embedded in data"),
	}
}

// ──────────────────────────────────────────────
// 3. ENCODING ATTACKS — obfuscation techniques
// ──────────────────────────────────────────────

func encodingAttackPatterns() []regexPattern {
	cat := "encoded_payload"
	return []regexPattern{
		compile(`[A-Za-z0-9+/]{60,}={0,2}`, cat, SeverityMedium, 0.7,
			"Possible Base64 encoded payload (60+ chars)"),
		compile(`(\\x[0-9a-fA-F]{2}){8,}`, cat, SeverityMedium, 0.7,
			"Hex-escaped byte sequence"),
		compile(`(%[0-9a-fA-F]{2}){8,}`, cat, SeverityMedium, 0.65,
			"URL-encoded byte sequence"),
		compile(`(\\u[0-9a-fA-F]{4}){4,}`, cat, SeverityMedium, 0.7,
			"Unicode escape sequence"),
		compile(`\brot13\s*[:(]`, cat, SeverityMedium, 0.75,
			"ROT13 encoding reference"),
		// Homoglyph attacks — mixing Cyrillic/Latin in the same field
		compile(`[\x{0410}-\x{044F}].*[a-z].*ignore|ignore.*[\x{0410}-\x{044F}]`, cat, SeverityMedium, 0.65,
			"Mixed Cyrillic/Latin (possible homoglyph attack)"),
		// Leetspeak obfuscation of suppression keywords
		compile(`1gn0r3|f0rg3t|0v3rr1d3|byp4ss|d1s4bl3|d1sr3g4rd`, cat, SeverityMedium, 0.7,
			"Leetspeak obfuscation of injection keywords"),
	}
}

// ──────────────────────────────────────────────
// 4. SQL INJECTION
// ──────────────────────────────────────────────

func sqlInjectionPatterns() []regexPattern {
	cat := "sql_injection"
	return []regexPattern{
		compile(`;\s*(drop|delete|truncate|alter|update|insert|create|exec|execute)\s+`, cat, SeverityCritical, 0.9,
			"SQL DML/DDL after semicolon"),
		compile(`'\s*(or|and)\s+['"]?\d+['"]?\s*=\s*['"]?\d+`, cat, SeverityHigh, 0.85,
			"SQL tautology (OR 1=1)"),
		compile(`'\s*(or|and)\s+['"]?[a-z]+['"]?\s*=\s*['"]?[a-z]+['"]?\s*(--)?\s*$`, cat, SeverityHigh, 0.8,
			"SQL tautology (OR 'a'='a')"),
		compile(`union\s+(all\s+)?select\s+`, cat, SeverityHigh, 0.85,
			"UNION SELECT"),
		compile(`;\s*--\s*$`, cat, SeverityMedium, 0.7,
			"SQL comment terminator"),
		compile(`/\*.*\*/`, cat, SeverityMedium, 0.6,
			"SQL block comment"),
		compile(`\bwaitfor\s+delay\b`, cat, SeverityHigh, 0.85,
			"SQL time-based blind injection (WAITFOR)"),
		compile(`\bsleep\s*\(\s*\d+\s*\)`, cat, SeverityHigh, 0.85,
			"SQL time-based blind injection (SLEEP)"),
		compile(`\bbenchmark\s*\(`, cat, SeverityHigh, 0.85,
			"SQL time-based blind injection (BENCHMARK)"),
		compile(`\bload_file\s*\(`, cat, SeverityCritical, 0.9,
			"SQL file read (LOAD_FILE)"),
		compile(`\binto\s+(out|dump)file\b`, cat, SeverityCritical, 0.9,
			"SQL file write (INTO OUTFILE)"),
		compile(`\bconcat\s*\(.*select\b`, cat, SeverityHigh, 0.8,
			"SQL nested query in CONCAT"),
		compile(`\binformation_schema\b`, cat, SeverityHigh, 0.85,
			"SQL metadata access (information_schema)"),
		compile(`\bpg_catalog\b`, cat, SeverityHigh, 0.85,
			"PostgreSQL catalog access"),
	}
}

// ──────────────────────────────────────────────
// 5. COMMAND INJECTION
// ──────────────────────────────────────────────

func commandInjectionPatterns() []regexPattern {
	cat := "command_injection"
	return []regexPattern{
		compile(`;\s*(cat|ls|dir|whoami|id|pwd|uname|ifconfig|ipconfig|hostname)\b`, cat, SeverityCritical, 0.9,
			"Shell command after semicolon"),
		compile(`\|\s*(cat|sh|bash|cmd|powershell|python|perl|ruby|node)\b`, cat, SeverityCritical, 0.9,
			"Piped shell command"),
		compile("`[^`]*(cat|sh|bash|rm|wget|curl|nc|ncat)[^`]*`", cat, SeverityCritical, 0.9,
			"Command in backticks"),
		compile(`\$\([^)]*\b(sh|bash|cat|wget|curl|rm|chmod|chown)\b`, cat, SeverityCritical, 0.9,
			"Command in $() substitution"),
		compile(`\b(rm|chmod|chown|kill|pkill|mkfs)\s+(-[a-z]+\s+)?/`, cat, SeverityCritical, 0.9,
			"Destructive command on absolute path"),
		compile(`\beval\s*\(`, cat, SeverityHigh, 0.8,
			"Dynamic code execution (eval)"),
		compile(`\bexec\s*\(`, cat, SeverityHigh, 0.8,
			"Process execution (exec)"),
		compile(`\bos\.(system|popen|exec|spawn)\s*\(`, cat, SeverityCritical, 0.9,
			"Python OS command execution"),
		compile(`\bsubprocess\.(call|run|Popen|check_output)\s*\(`, cat, SeverityCritical, 0.9,
			"Python subprocess execution"),
		compile(`\brequire\s*\(\s*['"]child_process['"]`, cat, SeverityCritical, 0.9,
			"Node.js child_process import"),
	}
}

// ──────────────────────────────────────────────
// 6. PATH TRAVERSAL
// ──────────────────────────────────────────────

func pathTraversalPatterns() []regexPattern {
	cat := "path_traversal"
	return []regexPattern{
		compile(`\.\./\.\./`, cat, SeverityHigh, 0.85,
			"Directory traversal (../../)"),
		compile(`\.\.\\\.\.\\`, cat, SeverityHigh, 0.85,
			"Directory traversal (..\\..\\)"),
		compile(`%2e%2e[%/]`, cat, SeverityHigh, 0.85,
			"URL-encoded directory traversal"),
		compile(`/etc/(passwd|shadow|hosts|sudoers)`, cat, SeverityCritical, 0.95,
			"Access to sensitive system file"),
		compile(`[a-zA-Z]:\\(windows|users|program\s?files)\\`, cat, SeverityHigh, 0.8,
			"Access to Windows system path"),
		compile(`~/(\.ssh|\.aws|\.env|\.git|\.bashrc|\.profile)`, cat, SeverityHigh, 0.85,
			"Access to dotfile/secrets"),
	}
}

// ──────────────────────────────────────────────
// 7. XSS / HTML INJECTION
// ──────────────────────────────────────────────

func xssPatterns() []regexPattern {
	cat := "xss"
	return []regexPattern{
		compile(`<script[^>]*>`, cat, SeverityHigh, 0.85, "Script tag"),
		compile(`javascript\s*:`, cat, SeverityHigh, 0.8, "javascript: URI"),
		compile(`\bon\w+\s*=\s*["']`, cat, SeverityMedium, 0.7,
			"Inline event handler (onclick, onerror, etc.)"),
		compile(`<iframe[^>]*>`, cat, SeverityHigh, 0.8, "Iframe injection"),
		compile(`<object[^>]*>`, cat, SeverityMedium, 0.7, "Object tag injection"),
		compile(`<embed[^>]*>`, cat, SeverityMedium, 0.7, "Embed tag injection"),
		compile(`<svg[^>]*on\w+\s*=`, cat, SeverityHigh, 0.85,
			"SVG with event handler"),
		compile(`<img[^>]*onerror\s*=`, cat, SeverityHigh, 0.85,
			"Image onerror handler"),
		compile(`expression\s*\(`, cat, SeverityMedium, 0.7, "CSS expression (IE)"),
		compile(`@import\s+(url\s*\(|['"])`, cat, SeverityMedium, 0.65,
			"CSS import injection"),
	}
}

// ──────────────────────────────────────────────
// 8. DATA EXFILTRATION
// ──────────────────────────────────────────────

func dataExfiltrationPatterns() []regexPattern {
	cat := "data_exfiltration"
	return []regexPattern{
		compile(`(curl|wget|fetch|http\.get|requests\.get|requests\.post)\s+https?://`, cat, SeverityHigh, 0.8,
			"HTTP request to external URL"),
		compile(`\bnc\s+-[a-z]*\s+\d+\.\d+\.\d+\.\d+`, cat, SeverityCritical, 0.9,
			"Netcat connection to IP"),
		compile(`\b(send|post|upload|transmit|forward)\s+(all\s+)?(the\s+)?(data|info|content|files?|secrets?|passwords?|tokens?|keys?)`, cat, SeverityHigh, 0.8,
			"Data exfiltration instruction"),
		compile(`\bexfiltrat`, cat, SeverityCritical, 0.95,
			"Explicit exfiltration keyword"),
		compile(`\b(api[_-]?key|secret[_-]?key|access[_-]?token|private[_-]?key|auth[_-]?token)\s*[:=]`, cat, SeverityHigh, 0.8,
			"Credential pattern in data"),
		compileSensitive(`(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,}`, cat, SeverityCritical, 0.95,
			"GitHub token pattern"),
		compileSensitive(`(sk|pk)_(live|test)_[A-Za-z0-9]{20,}`, cat, SeverityCritical, 0.95,
			"Stripe API key pattern"),
		compileSensitive(`AKIA[0-9A-Z]{16}`, cat, SeverityCritical, 0.95,
			"AWS access key pattern"),
		compileSensitive(`xox[bpras]-[0-9a-zA-Z-]{10,}`, cat, SeverityCritical, 0.95,
			"Slack token pattern"),
	}
}

// ──────────────────────────────────────────────
// 9. SUSPICIOUS URLs
// ──────────────────────────────────────────────

func suspiciousURLPatterns() []regexPattern {
	cat := "suspicious_url"
	return []regexPattern{
		compile(`https?://[^\s]+\.(ru|cn|tk|ml|ga|cf|top|xyz|pw|cc|ws)/`, cat, SeverityMedium, 0.65,
			"URL with high-risk TLD"),
		compile(`https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`, cat, SeverityMedium, 0.7,
			"URL using raw IP address"),
		compile(`https?://[^\s]*\.(onion|i2p)\b`, cat, SeverityHigh, 0.85,
			"Tor/I2P hidden service URL"),
		compile(`https?://bit\.ly/|https?://tinyurl\.com/|https?://t\.co/|https?://goo\.gl/`, cat, SeverityLow, 0.5,
			"URL shortener (could mask destination)"),
		compile(`https?://[^\s]*@[^\s]*`, cat, SeverityMedium, 0.7,
			"URL with embedded credentials"),
		compile(`https?://[^\s]*\.(exe|bat|cmd|ps1|sh|bin|msi|dll|scr)\b`, cat, SeverityHigh, 0.85,
			"URL pointing to executable file"),
		compile(`data:text/html`, cat, SeverityHigh, 0.8,
			"Data URI with HTML content"),
	}
}

// ──────────────────────────────────────────────
// 10. PII EXPOSURE
// ──────────────────────────────────────────────

func piiPatterns() []regexPattern {
	return []regexPattern{
		compileSensitive(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
			"email_address", SeverityMedium, 0.85, "Email address"),
		compileSensitive(`(\+?\d{1,3}[\s.-]?)?\(?\d{3}\)?[\s.-]?\d{3}[\s.-]?\d{4}`,
			"phone_number", SeverityMedium, 0.75, "Phone number"),
		compileSensitive(`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`,
			"credit_card", SeverityHigh, 0.85, "Credit card number"),
		compileSensitive(`\b\d{3}-\d{2}-\d{4}\b`,
			"ssn", SeverityHigh, 0.9, "US Social Security Number"),
		compileSensitive(`\b[A-Z]{2}\d{2}\s?[A-Z0-9]{4}\s?\d{4}\s?\d{4}\s?\d{4}\s?\d{0,4}\s?\d{0,2}\b`,
			"iban", SeverityHigh, 0.8, "IBAN bank account number"),
		compileSensitive(`\b\d{3}\s?\d{3}\s?\d{3}\s?\d{2}\b`,
			"nir", SeverityHigh, 0.7, "French NIR (social security number)"),
	}
}
