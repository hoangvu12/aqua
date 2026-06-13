package riot

import "strings"

// Endpoints is the GLZ region/shard pair derived from the account region.
// Ported verbatim from the reference Get-GlzEndpoints: vn2/vn → ap/ap,
// latam → latam/na, br → br/na, otherwise region == shard.
type Endpoints struct {
	GLZRegion string
	Shard     string
}

// MapRegion maps a raw region-locale region to GLZ endpoints.
func MapRegion(region string) Endpoints {
	r := strings.ToLower(region)
	switch r {
	case "vn2", "vn":
		return Endpoints{GLZRegion: "ap", Shard: "ap"}
	case "latam":
		return Endpoints{GLZRegion: "latam", Shard: "na"}
	case "br":
		return Endpoints{GLZRegion: "br", Shard: "na"}
	default:
		return Endpoints{GLZRegion: r, Shard: r}
	}
}

// GLZHost is the game-data host, e.g. glz-ap-1.ap.a.pvp.net.
func (e Endpoints) GLZHost() string {
	return "glz-" + e.GLZRegion + "-1." + e.Shard + ".a.pvp.net"
}

// PDHost is the player-data host, e.g. pd.ap.a.pvp.net.
func (e Endpoints) PDHost() string {
	return "pd." + e.Shard + ".a.pvp.net"
}
