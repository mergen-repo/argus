package rattype

import "strings"

const (
	UTRAN   = "utran"
	GERAN   = "geran"
	LTE     = "lte"
	NBIOT   = "nb_iot"
	LTEM    = "lte_m"
	NR5G    = "nr_5g"
	NR5GNSA = "nr_5g_nsa"
	Unknown = "unknown"
)

var canonicalSet = map[string]bool{
	UTRAN:   true,
	GERAN:   true,
	LTE:     true,
	NBIOT:   true,
	LTEM:    true,
	NR5G:    true,
	NR5GNSA: true,
	Unknown: true,
}

var aliasMap = map[string]string{
	"utran":     UTRAN,
	"geran":     GERAN,
	"lte":       LTE,
	"nb_iot":    NBIOT,
	"lte_m":     LTEM,
	"nr_5g":     NR5G,
	"nr_5g_nsa": NR5GNSA,
	"unknown":   Unknown,

	"2g":      GERAN,
	"3g":      UTRAN,
	"4g":      LTE,
	"5g":      NR5G,
	"5g_sa":   NR5G,
	"5g_nsa":  NR5GNSA,
	"cat_m1":  LTEM,
	"nb-iot":  NBIOT,
	"nbiot":   NBIOT,
	"eutran":  LTE,
	"e-utran": LTE,
	"nr":      NR5G,
}

var displayNames = map[string]string{
	UTRAN:   "3G",
	GERAN:   "2G",
	LTE:     "4G",
	NBIOT:   "NB-IoT",
	LTEM:    "LTE-M",
	NR5G:    "5G",
	NR5GNSA: "5G-NSA",
	Unknown: "Unknown",
}

var displayToCanonical = map[string]string{
	"3g":     UTRAN,
	"2g":     GERAN,
	"4g":     LTE,
	"nb-iot": NBIOT,
	"lte-m":  LTEM,
	"5g":     NR5G,
	"5g-nsa": NR5GNSA,
}

func FromRADIUS(rawValue uint8) string {
	switch rawValue {
	case 1:
		return UTRAN
	case 2:
		return GERAN
	case 6:
		return LTE
	case 7:
		return NR5G
	case 8:
		return NR5GNSA
	case 9:
		return NBIOT
	case 10:
		return LTEM
	default:
		return Unknown
	}
}

func FromDiameter(rawValue uint32) string {
	switch rawValue {
	case 1000:
		return UTRAN
	case 1001:
		return GERAN
	case 1004:
		return LTE
	case 1005:
		return NBIOT
	case 1006:
		return LTEM
	case 1008:
		return NR5GNSA
	case 1009:
		return NR5G
	default:
		return Unknown
	}
}

func FromSBA(rawString string) string {
	lower := strings.ToLower(strings.TrimSpace(rawString))

	switch lower {
	case "nr", "nr_5g":
		return NR5G
	case "e-utra-nr", "eutra-nr":
		return NR5GNSA
	case "e-utra", "eutra", "lte":
		return LTE
	case "nb-iot", "nb_iot", "nbiot":
		return NBIOT
	case "lte-m", "lte_m", "cat-m1", "cat_m1":
		return LTEM
	}

	if v, ok := aliasMap[lower]; ok {
		return v
	}

	return Unknown
}

func Normalize(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))

	if canonicalSet[lower] {
		return lower
	}

	if v, ok := aliasMap[lower]; ok {
		return v
	}

	if v, ok := displayToCanonical[lower]; ok {
		return v
	}

	return Unknown
}

func DisplayName(canonical string) string {
	if name, ok := displayNames[canonical]; ok {
		return name
	}
	return "Unknown"
}

func IsValid(value string) bool {
	return canonicalSet[value]
}

func AllCanonical() []string {
	return []string{UTRAN, GERAN, LTE, NBIOT, LTEM, NR5G, NR5GNSA, Unknown}
}

func AllDisplayNames() map[string]string {
	result := make(map[string]string, len(displayNames))
	for k, v := range displayNames {
		result[k] = v
	}
	return result
}
