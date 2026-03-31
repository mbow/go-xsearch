// cmd/generate_products/main.go generates a large products.json with ~10,000 beers
// plus existing non-beer products, using consistent limited-vocabulary tags
// for optimal search matching.
//
// Usage: go run cmd/generate_products/main.go > data/products.json
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
)

type Product struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Tags     []string `json:"tags,omitempty"`
}

// Style defines a beer style with its typical attributes.
type Style struct {
	Name        string
	Type        string   // lager, ale
	Color       string   // light, golden, amber, copper, dark, black, hazy
	Flavors     []string // from limited vocabulary
	ABVLow      float64
	ABVHigh     float64
	Carbonation string // high, medium, low, nitro
}

var styles = []Style{
	{"Lager", "lager", "light", []string{"clean", "crisp", "refreshing"}, 4.0, 5.5, "high"},
	{"Light Lager", "lager", "light", []string{"clean", "crisp", "light"}, 3.5, 4.5, "high"},
	{"Pilsner", "lager", "light", []string{"clean", "crisp", "hoppy", "dry"}, 4.0, 5.5, "high"},
	{"Helles", "lager", "golden", []string{"clean", "malty", "refreshing"}, 4.5, 5.5, "medium"},
	{"Dunkel", "lager", "dark", []string{"malty", "chocolate", "roasted"}, 4.5, 5.5, "medium"},
	{"Marzen", "lager", "amber", []string{"malty", "clean", "sweet"}, 5.0, 6.5, "medium"},
	{"Bock", "lager", "copper", []string{"malty", "sweet", "strong"}, 6.0, 7.5, "medium"},
	{"Doppelbock", "lager", "dark", []string{"malty", "sweet", "strong", "roasted"}, 7.0, 10.0, "low"},
	{"Vienna Lager", "lager", "amber", []string{"malty", "clean", "dry"}, 4.5, 5.5, "medium"},
	{"Schwarzbier", "lager", "dark", []string{"clean", "roasted", "dry"}, 4.0, 5.5, "medium"},
	{"IPA", "ale", "golden", []string{"hoppy", "bitter", "citrus"}, 5.5, 7.5, "medium"},
	{"Double IPA", "ale", "golden", []string{"hoppy", "bitter", "strong", "dank"}, 7.5, 10.0, "medium"},
	{"Session IPA", "ale", "golden", []string{"hoppy", "light", "citrus"}, 3.5, 5.0, "high"},
	{"New England IPA", "ale", "hazy", []string{"hoppy", "tropical", "fruity", "creamy"}, 5.5, 7.5, "medium"},
	{"West Coast IPA", "ale", "golden", []string{"hoppy", "bitter", "citrus", "dry"}, 6.0, 7.5, "medium"},
	{"Pale Ale", "ale", "golden", []string{"hoppy", "malty", "citrus"}, 4.5, 6.0, "medium"},
	{"American Pale Ale", "ale", "golden", []string{"hoppy", "citrus", "floral"}, 4.5, 6.0, "medium"},
	{"English Bitter", "ale", "amber", []string{"malty", "hoppy", "dry"}, 3.5, 5.0, "low"},
	{"Amber Ale", "ale", "amber", []string{"malty", "hoppy", "sweet"}, 4.5, 6.0, "medium"},
	{"Red Ale", "ale", "copper", []string{"malty", "sweet", "roasted"}, 4.5, 6.0, "medium"},
	{"Brown Ale", "ale", "dark", []string{"malty", "chocolate", "sweet"}, 4.5, 6.0, "medium"},
	{"Stout", "ale", "black", []string{"roasted", "coffee", "chocolate", "creamy"}, 4.0, 6.0, "low"},
	{"Dry Stout", "ale", "black", []string{"roasted", "coffee", "dry"}, 4.0, 5.0, "low"},
	{"Imperial Stout", "ale", "black", []string{"roasted", "coffee", "chocolate", "strong"}, 8.0, 12.0, "low"},
	{"Milk Stout", "ale", "black", []string{"roasted", "sweet", "creamy", "chocolate"}, 4.0, 6.0, "low"},
	{"Oatmeal Stout", "ale", "black", []string{"roasted", "creamy", "smooth"}, 4.0, 6.0, "low"},
	{"Porter", "ale", "dark", []string{"roasted", "chocolate", "malty"}, 4.5, 6.5, "medium"},
	{"Baltic Porter", "ale", "dark", []string{"roasted", "malty", "sweet", "strong"}, 6.0, 9.0, "medium"},
	{"Hefeweizen", "ale", "hazy", []string{"fruity", "spicy", "refreshing"}, 4.5, 5.5, "high"},
	{"Witbier", "ale", "hazy", []string{"light", "citrus", "spicy", "refreshing"}, 4.5, 5.5, "high"},
	{"Belgian Tripel", "ale", "golden", []string{"fruity", "spicy", "strong", "dry"}, 7.5, 10.0, "high"},
	{"Belgian Dubbel", "ale", "copper", []string{"fruity", "malty", "sweet", "spicy"}, 6.0, 8.0, "medium"},
	{"Belgian Blonde", "ale", "golden", []string{"fruity", "malty", "refreshing"}, 6.0, 7.5, "high"},
	{"Saison", "ale", "golden", []string{"fruity", "spicy", "dry", "refreshing"}, 5.0, 8.0, "high"},
	{"Sour Ale", "ale", "golden", []string{"sour", "tart", "fruity"}, 3.0, 6.0, "medium"},
	{"Gose", "ale", "hazy", []string{"sour", "salty", "citrus", "refreshing"}, 4.0, 5.5, "medium"},
	{"Berliner Weisse", "ale", "hazy", []string{"sour", "tart", "light", "refreshing"}, 2.5, 4.0, "high"},
	{"Lambic", "ale", "golden", []string{"sour", "tart", "funky"}, 4.0, 6.0, "low"},
	{"Fruit Beer", "ale", "hazy", []string{"fruity", "sweet", "refreshing"}, 4.0, 6.0, "medium"},
	{"Cream Ale", "ale", "light", []string{"clean", "crisp", "light", "refreshing"}, 4.0, 5.5, "high"},
	{"Kolsch", "ale", "light", []string{"clean", "crisp", "light", "dry"}, 4.5, 5.5, "high"},
	{"Scotch Ale", "ale", "copper", []string{"malty", "sweet", "smoky", "strong"}, 6.0, 10.0, "low"},
	{"Barleywine", "ale", "copper", []string{"malty", "sweet", "strong", "fruity"}, 8.0, 12.0, "low"},
	{"Smoked Beer", "ale", "amber", []string{"smoky", "malty", "roasted"}, 4.5, 6.5, "medium"},
	{"Barrel-Aged", "ale", "dark", []string{"strong", "smooth", "vanilla", "sweet"}, 8.0, 14.0, "low"},
}

var breweries = []string{
	"Anchor", "Allagash", "Alpine", "Avery", "Ballast Point",
	"Bear Republic", "Bell's", "Bent Water", "Big Sky", "Birdsong",
	"Blue Moon", "Blue Point", "Boddingtons", "Boulevard", "BrewDog",
	"Breckenridge", "Brooklyn", "Burley Oak", "Captain Lawrence", "Cascade",
	"Central Waters", "Cigar City", "Clown Shoes", "Coronado", "Creature Comforts",
	"Crooked Stave", "Deschutes", "Dogfish Head", "Drake's", "Ecliptic",
	"Elysian", "Evil Twin", "Faction", "Figueroa Mountain", "Firestone Walker",
	"Flying Dog", "Flying Fish", "Founders", "Four Peaks", "Fremont",
	"Georgetown", "Goose Island", "Grand Teton", "Great Divide", "Great Lakes",
	"Green Flash", "Half Acre", "Harpoon", "Highland", "Holy Mountain",
	"Hop Valley", "Ithaca", "Jack's Abby", "Jester King", "Jolly Pumpkin",
	"Karl Strauss", "Kern River", "Kona", "Lagunitas", "Left Hand",
	"Lone Pine", "Lost Abbey", "Lucky Envelope", "Maine Beer", "Maui",
	"MadTree", "Magic Hat", "Modern Times", "Moonraker", "Mother Earth",
	"New Belgium", "New Glarus", "Night Shift", "Ninkasi", "Notch",
	"Odell", "Ommegang", "Oskar Blues", "Other Half", "Oxbow",
	"Perennial", "pFriem", "Pike", "Pizza Port", "Prairie",
	"Rahr", "Real Ale", "Revision", "Revolution", "Rhinegeist",
	"Rogue", "Russian River", "Saint Arnold", "Samuel Adams", "SanTan",
	"Schlafly", "Shiner", "Short's", "Sierra Nevada", "Sixpoint",
	"Smog City", "Societe", "Southern Tier", "Stillwater", "Stone",
	"SweetWater", "Taps", "Terrapin", "The Alchemist", "The Bruery",
	"The Lost Abbey", "Three Floyds", "Toppling Goliath", "Transmitter", "Tree House",
	"Trillium", "Trinity", "Troegs", "Two Roads", "Uinta",
	"Union", "Upland", "Urban Chestnut", "Victory", "Wachusett",
	"Wander", "Wasatch", "Weldwerks", "Westbrook", "Wicked Weed",
	"Wild Leap", "Wren House", "Yazoo", "Yuengling", "Zero Gravity",
	"10 Barrel", "21st Amendment", "3 Floyds", "4 Hands", "5 Rabbit",
	"Aberrant", "Against The Grain", "Altamont", "Aslin", "Atomic",
	"Austin Beerworks", "Back Forty", "Bad Martha", "Bag of Holding", "Barrel House",
	"Bay State", "Beachwood", "Blackberry Farm", "Blackrocks", "Blind Pig",
	"Blue Owl", "Bold Rock", "Bonfire", "Boxing Cat", "Brash",
	"Breakside", "Brieux Carre", "Burial", "Calusa", "Cape May",
	"Cellarmaker", "Cerebral", "Charles Towne", "City Built", "Cloudburst",
	"Commonwealth", "Community", "Conshohocken", "Counter Weight", "Crow Peak",
	"Dancing Gnome", "Decadent", "Deep Ellum", "Devil's Backbone", "Door County",
	"Double Mountain", "Dry Dock", "Eel River", "Evil Genius", "Exhibit A",
	"Fair State", "Fat Orange Cat", "Fernson", "Finback", "Five Boroughs",
	"Foam", "Foreign Objects", "Fort Point", "Freewheel", "Funky Buddha",
	"Gang of Blades", "Garage", "Gold Spot", "Golden Road", "Good Word",
	"Graft", "Gravity Works", "Grimm", "Hardywood", "Haymarket",
	"Heater Allen", "Heist", "Hoof Hearted", "Hop Butcher", "Hop Culture",
	"Humble Sea", "Idle Hands", "Imprint", "Industrial Arts", "Iron Hill",
	"Jacks Hard Cider", "Jailbreak", "Kangaroo", "Kiitos", "King Harbor",
	"Lamplighter", "Land Grant", "Lawson's Finest", "Level", "Li'l Beaver",
	"Local Brewing", "Lone Peak", "Long Trail", "Magnify", "Mast Landing",
	"Melvin", "Mighty Squirrel", "Monkish", "Mountain Culture", "Narrow Gauge",
	"New Park", "New Trail", "Nightmare", "Noon Whistle", "North Park",
	"Oak Highlands", "Offshoot", "Old Nation", "Outer Range", "Parish",
	"Phase Three", "Pinthouse", "Pipeworks", "Proclamation", "Pure Project",
	"Quaff", "Relic", "Resident Culture", "Rising Tide", "River Roost",
	"Roadmap", "Root Down", "Rowley Farmhouse", "Sapwood", "Scratch",
	"Second Self", "Seven Stills", "Shared", "Side Project", "SingleCut",
	"Sloop", "Southern Grist", "Speciation", "Stoup", "Sun King",
	"Suarez Family", "Surly", "Tavour", "Threes", "Timber",
	"Torch and Crown", "Trace", "Triple Crossing", "True Anomaly", "Turning Point",
	"Twin Elephant", "Untitled Art", "Urban South", "Veil", "Von Trapp",
	"Warped Wing", "Weathered Souls", "Weihenstephaner", "Wildeye", "Wolf's Ridge",
}

var nameModifiers = []string{
	"", "Reserve", "Special", "Limited", "Original",
	"Classic", "Premium", "Gold", "Silver", "Platinum",
	"Harvest", "Summer", "Winter", "Autumn", "Spring",
	"Double", "Triple", "Single", "Barrel", "Cask",
	"Dry Hopped", "Fresh", "Aged", "Small Batch", "Grand Cru",
	"Anniversary", "Celebration", "Flagship", "Seasonal", "Rare",
	"No. 1", "No. 5", "No. 9", "No. 12", "No. 23",
}

var extraIngredients = [][]string{
	nil, // most beers have no extras
	nil,
	nil,
	nil,
	nil,
	{"rice"},
	{"corn"},
	{"wheat"},
	{"oats"},
	{"rye"},
	{"honey"},
	{"coffee"},
	{"chocolate"},
	{"vanilla"},
	{"citrus peel"},
	{"coriander"},
	{"lactose"},
	{"coconut"},
	{"ginger"},
	{"cinnamon"},
	{"bourbon barrel"},
	{"maple"},
	{"pumpkin"},
	{"cherry"},
	{"raspberry"},
	{"mango"},
	{"passion fruit"},
	{"peach"},
	{"pineapple"},
	{"blood orange"},
}

var servings = []string{"bottle", "can", "draft"}
var sizes = []string{"12oz", "16oz", "12oz", "16oz", "pint", "22oz", "330ml", "500ml"}

func main() {
	rng := rand.New(rand.NewSource(42)) // deterministic for reproducibility

	var products []Product

	// Generate ~10,000 beers
	seen := make(map[string]struct{})
	for len(products) < 10000 {
		brewery := breweries[rng.Intn(len(breweries))]
		style := styles[rng.Intn(len(styles))]
		modifier := nameModifiers[rng.Intn(len(nameModifiers))]

		var name string
		if modifier == "" {
			name = fmt.Sprintf("%s %s", brewery, style.Name)
		} else {
			name = fmt.Sprintf("%s %s %s", brewery, modifier, style.Name)
		}

		// Skip duplicates
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		// Build tags with consistent limited vocabulary
		abv := style.ABVLow + rng.Float64()*(style.ABVHigh-style.ABVLow)
		abvStr := fmt.Sprintf("%.1f%%", abv)

		var abvCategory string
		switch {
		case abv < 4.0:
			abvCategory = "session"
		case abv < 5.5:
			abvCategory = "standard"
		case abv < 7.0:
			abvCategory = "strong"
		default:
			abvCategory = "imperial"
		}

		serving := servings[rng.Intn(len(servings))]
		size := sizes[rng.Intn(len(sizes))]

		tags := []string{style.Type, style.Color, style.Carbonation + " carbonation", abvStr, abvCategory, serving, size}
		tags = append(tags, style.Flavors...)

		// Maybe add an extra ingredient
		extras := extraIngredients[rng.Intn(len(extraIngredients))]
		if extras != nil {
			tags = append(tags, extras...)
		}

		products = append(products, Product{
			Name:     name,
			Category: "beer",
			Tags:     tags,
		})
	}

	// Add non-beer products (existing catalog items)
	nonBeer := []Product{
		{Name: "Jack Daniel's Tennessee Whiskey", Category: "whiskey", Tags: []string{"bourbon", "smooth", "amber", "40%", "bottle", "750ml"}},
		{Name: "Johnnie Walker Black Label", Category: "whiskey", Tags: []string{"scotch", "smoky", "dark", "40%", "bottle", "750ml"}},
		{Name: "Jameson Irish Whiskey", Category: "whiskey", Tags: []string{"irish", "smooth", "light", "40%", "bottle", "750ml"}},
		{Name: "Maker's Mark Bourbon", Category: "whiskey", Tags: []string{"bourbon", "sweet", "amber", "45%", "bottle", "750ml"}},
		{Name: "Buffalo Trace Bourbon", Category: "whiskey", Tags: []string{"bourbon", "smooth", "amber", "45%", "bottle", "750ml"}},
		{Name: "Grey Goose Vodka", Category: "vodka", Tags: []string{"smooth", "clean", "light", "40%", "bottle", "750ml"}},
		{Name: "Absolut Vodka", Category: "vodka", Tags: []string{"clean", "crisp", "light", "40%", "bottle", "750ml"}},
		{Name: "Tito's Handmade Vodka", Category: "vodka", Tags: []string{"smooth", "clean", "light", "40%", "bottle", "750ml"}},
		{Name: "Tanqueray London Dry Gin", Category: "gin", Tags: []string{"dry", "citrus", "floral", "47%", "bottle", "750ml"}},
		{Name: "Hendrick's Gin", Category: "gin", Tags: []string{"floral", "refreshing", "light", "44%", "bottle", "750ml"}},
		{Name: "Bombay Sapphire", Category: "gin", Tags: []string{"citrus", "dry", "light", "47%", "bottle", "750ml"}},
		{Name: "Patron Silver Tequila", Category: "tequila", Tags: []string{"smooth", "citrus", "light", "40%", "bottle", "750ml"}},
		{Name: "Don Julio Blanco", Category: "tequila", Tags: []string{"clean", "citrus", "light", "40%", "bottle", "750ml"}},
		{Name: "Casamigos Blanco", Category: "tequila", Tags: []string{"smooth", "sweet", "light", "40%", "bottle", "750ml"}},
		{Name: "Bacardi Superior Rum", Category: "rum", Tags: []string{"light", "clean", "sweet", "40%", "bottle", "750ml"}},
		{Name: "Captain Morgan Spiced Rum", Category: "rum", Tags: []string{"spicy", "sweet", "amber", "35%", "bottle", "750ml"}},
		{Name: "Aperol", Category: "liqueur", Tags: []string{"bitter", "sweet", "citrus", "light", "11%", "bottle", "750ml"}},
		{Name: "Kahlua Coffee Liqueur", Category: "liqueur", Tags: []string{"coffee", "sweet", "dark", "20%", "bottle", "750ml"}},
		{Name: "Baileys Irish Cream", Category: "liqueur", Tags: []string{"creamy", "sweet", "chocolate", "17%", "bottle", "750ml"}},
		{Name: "White Claw Hard Seltzer", Category: "hard seltzer", Tags: []string{"light", "crisp", "refreshing", "5%", "can", "12oz"}},
		{Name: "Truly Hard Seltzer", Category: "hard seltzer", Tags: []string{"light", "fruity", "refreshing", "5%", "can", "12oz"}},
		{Name: "Angry Orchard Crisp Apple", Category: "cider", Tags: []string{"sweet", "fruity", "refreshing", "5%", "bottle", "12oz"}},
		{Name: "Nike Air Max", Category: "shoes"},
		{Name: "Nike Dunk Low", Category: "shoes"},
		{Name: "Adidas Superstar", Category: "shoes"},
		{Name: "Samsung Galaxy S24", Category: "phone"},
		{Name: "iPhone 16 Pro", Category: "phone"},
		{Name: "Google Pixel 9", Category: "phone"},
		{Name: "Sony WH-1000XM5", Category: "headphones"},
		{Name: "AirPods Pro", Category: "headphones"},
		{Name: "Bose QuietComfort", Category: "headphones"},
		{Name: "Coca-Cola", Category: "soda"},
		{Name: "Pepsi", Category: "soda"},
		{Name: "Sprite", Category: "soda"},
		{Name: "Mountain Dew", Category: "soda"},
	}
	products = append(products, nonBeer...)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(products); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}
