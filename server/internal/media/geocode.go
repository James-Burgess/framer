package media

import (
	"fmt"
	"math"
)

type city struct {
	Name string
	Lat  float64
	Lon  float64
}

// Offline nearest-city table so EXIF GPS coordinates display human-friendly
// places without making any outbound network calls.
var worldCities = []city{
	// South Africa & Southern Africa
	{"Cape Town, South Africa", -33.9249, 18.4241},
	{"Johannesburg, South Africa", -26.2041, 28.0473},
	{"Pretoria, South Africa", -25.7479, 28.2293},
	{"Durban, South Africa", -29.8587, 31.0218},
	{"Gqeberha, South Africa", -33.9608, 25.6022},
	{"Bloemfontein, South Africa", -29.0852, 26.1596},
	{"Stellenbosch, South Africa", -33.9321, 18.8602},
	{"Hermanus, South Africa", -34.4187, 19.2345},
	{"Knysna, South Africa", -34.0363, 23.0471},
	{"George, South Africa", -33.9630, 22.4617},
	{"Mbombela, South Africa", -25.4753, 30.9694},
	{"Polokwane, South Africa", -23.9045, 29.4689},
	{"East London, South Africa", -33.0153, 27.9116},
	{"Windhoek, Namibia", -22.5609, 17.0658},
	{"Swakopmund, Namibia", -22.6792, 14.5272},
	{"Gaborone, Botswana", -24.6282, 25.9231},
	{"Victoria Falls, Zimbabwe", -17.9243, 25.8572},
	{"Harare, Zimbabwe", -17.8252, 31.0335},
	{"Maputo, Mozambique", -25.9692, 32.5732},
	{"Nairobi, Kenya", -1.2921, 36.8219},
	{"Cairo, Egypt", 30.0444, 31.2357},
	{"Marrakech, Morocco", 31.6295, -7.9811},

	// Europe
	{"London, UK", 51.5074, -0.1278},
	{"Edinburgh, UK", 55.9533, -3.1883},
	{"Manchester, UK", 53.4808, -2.2426},
	{"Dublin, Ireland", 53.3498, -6.2603},
	{"Paris, France", 48.8566, 2.3522},
	{"Nice, France", 43.7102, 7.2620},
	{"Lyon, France", 45.7640, 4.8357},
	{"Amsterdam, Netherlands", 52.3676, 4.9041},
	{"Brussels, Belgium", 50.8503, 4.3517},
	{"Berlin, Germany", 52.5200, 13.4050},
	{"Munich, Germany", 48.1351, 11.5820},
	{"Frankfurt, Germany", 50.1109, 8.6821},
	{"Hamburg, Germany", 53.5511, 9.9937},
	{"Zurich, Switzerland", 47.3769, 8.5417},
	{"Geneva, Switzerland", 46.2044, 6.1432},
	{"Vienna, Austria", 48.2082, 16.3738},
	{"Prague, Czechia", 50.0755, 14.4378},
	{"Rome, Italy", 41.9028, 12.4964},
	{"Milan, Italy", 45.4642, 9.1900},
	{"Venice, Italy", 45.4408, 12.3155},
	{"Florence, Italy", 43.7696, 11.2558},
	{"Naples, Italy", 40.8518, 14.2681},
	{"Barcelona, Spain", 41.3874, 2.1686},
	{"Madrid, Spain", 40.4168, -3.7038},
	{"Seville, Spain", 37.3891, -5.9845},
	{"Lisbon, Portugal", 38.7223, -9.1393},
	{"Porto, Portugal", 41.1579, -8.6291},
	{"Athens, Greece", 37.9838, 23.7275},
	{"Santorini, Greece", 36.3932, 25.4615},
	{"Istanbul, Turkey", 41.0082, 28.9784},
	{"Stockholm, Sweden", 59.3293, 18.0686},
	{"Copenhagen, Denmark", 55.6761, 12.5683},
	{"Oslo, Norway", 59.9139, 10.7522},
	{"Helsinki, Finland", 60.1699, 24.9384},
	{"Reykjavik, Iceland", 64.1466, -21.9426},
	{"Warsaw, Poland", 52.2297, 21.0122},
	{"Budapest, Hungary", 47.4979, 19.0402},
	{"Dubrovnik, Croatia", 42.6507, 18.0944},

	// North America
	{"New York, USA", 40.7128, -74.0060},
	{"Boston, USA", 42.3601, -71.0589},
	{"Washington, D.C., USA", 38.9072, -77.0369},
	{"Miami, USA", 25.7617, -80.1918},
	{"Atlanta, USA", 33.7490, -84.3880},
	{"Chicago, USA", 41.8781, -87.6298},
	{"Austin, USA", 30.2672, -97.7431},
	{"Dallas, USA", 32.7767, -96.7970},
	{"Houston, USA", 29.7604, -95.3698},
	{"Denver, USA", 39.7392, -104.9903},
	{"Phoenix, USA", 33.4484, -112.0740},
	{"Las Vegas, USA", 36.1699, -115.1398},
	{"Los Angeles, USA", 34.0522, -118.2437},
	{"San Diego, USA", 32.7157, -117.1611},
	{"San Francisco, USA", 37.7749, -122.4194},
	{"Seattle, USA", 47.6062, -122.3321},
	{"Portland, USA", 45.5152, -122.6784},
	{"Honolulu, USA", 21.3069, -157.8583},
	{"Toronto, Canada", 43.6532, -79.3832},
	{"Montreal, Canada", 45.5017, -73.5673},
	{"Vancouver, Canada", 49.2827, -123.1207},
	{"Calgary, Canada", 51.0447, -114.0719},
	{"Banff, Canada", 51.1784, -115.5708},
	{"Mexico City, Mexico", 19.4326, -99.1332},
	{"Cancun, Mexico", 21.1619, -86.8515},

	// South America
	{"Rio de Janeiro, Brazil", -22.9068, -43.1729},
	{"Sao Paulo, Brazil", -23.5505, -46.6333},
	{"Buenos Aires, Argentina", -34.6037, -58.3816},
	{"Santiago, Chile", -33.4489, -70.6693},
	{"Lima, Peru", -12.0464, -77.0428},
	{"Cusco, Peru", -13.5319, -71.9675},
	{"Bogota, Colombia", 4.7110, -74.0721},

	// Asia & Middle East
	{"Dubai, UAE", 25.2048, 55.2708},
	{"Doha, Qatar", 25.2854, 51.5310},
	{"Tel Aviv, Israel", 32.0853, 34.7818},
	{"Mumbai, India", 19.0760, 72.8777},
	{"New Delhi, India", 28.6139, 77.2090},
	{"Bengaluru, India", 12.9716, 77.5946},
	{"Bangkok, Thailand", 13.7563, 100.5018},
	{"Phuket, Thailand", 7.8804, 98.3923},
	{"Singapore", 1.3521, 103.8198},
	{"Kuala Lumpur, Malaysia", 3.1390, 101.6869},
	{"Bali, Indonesia", -8.4095, 115.1889},
	{"Hanoi, Vietnam", 21.0278, 105.8342},
	{"Ho Chi Minh City, Vietnam", 10.8231, 106.6297},
	{"Hong Kong", 22.3193, 114.1694},
	{"Taipei, Taiwan", 25.0330, 121.5654},
	{"Shanghai, China", 31.2304, 121.4737},
	{"Beijing, China", 39.9042, 116.4074},
	{"Seoul, South Korea", 37.5665, 126.9780},
	{"Tokyo, Japan", 35.6762, 139.6503},
	{"Kyoto, Japan", 35.0116, 135.7681},
	{"Osaka, Japan", 34.6937, 135.5023},

	// Oceania
	{"Sydney, Australia", -33.8688, 151.2093},
	{"Melbourne, Australia", -37.8136, 144.9631},
	{"Brisbane, Australia", -27.4698, 153.0251},
	{"Perth, Australia", -31.9505, 115.8605},
	{"Auckland, New Zealand", -36.8509, 174.7645},
	{"Queenstown, New Zealand", -45.0312, 168.6626},
	{"Wellington, New Zealand", -41.2865, 174.7762},
}

// LookupLocation returns the nearest city within 90 km or formatted coordinates.
func LookupLocation(lat, lon float64) string {
	bestDist := 90.0
	bestName := ""
	for _, c := range worldCities {
		d := haversineKm(lat, lon, c.Lat, c.Lon)
		if d < bestDist {
			bestDist = d
			bestName = c.Name
		}
	}
	if bestName != "" {
		return bestName
	}
	ns := "N"
	if lat < 0 {
		ns = "S"
		lat = -lat
	}
	ew := "E"
	if lon < 0 {
		ew = "W"
		lon = -lon
	}
	return fmt.Sprintf("%.2f° %s, %.2f° %s", lat, ns, lon, ew)
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
