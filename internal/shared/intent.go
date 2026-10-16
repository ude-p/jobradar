package shared

type Intent struct {
	Skills     []string
	Categories []string
	Terms      []string
	// Location is a free-text place name; Countries and Regions are codes.
	Location         []string
	Regions          []string
	Countries        []string
	WorkMode         WorkMode
	PostedWithinDays int
	// Limit is the remaining candidate budget the harvester passes down, not how
	// many are delivered.
	Limit int
}

type Criteria struct {
	Context   string
	Questions map[string]Question
}

type Question struct {
	// Name keys the question and its answer, but is never sent: the request
	// carries its questions as a map.
	Name         string `json:"-"`
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type Queue struct {
	Name     string
	Search   Intent
	Criteria Criteria
	ChatIDs  []string
}

func Crit() *crit {
	return &crit{}
}

type crit struct {
	criteria Criteria
}

func (builder *crit) Context(text string) *crit {
	builder.criteria.Context = text
	return builder
}

func (builder *crit) Questions(questions ...Question) Criteria {
	builder.criteria.Questions = make(map[string]Question, len(questions))
	for _, question := range questions {
		builder.criteria.Questions[question.Name] = question
	}
	return builder.criteria
}

func Choice(name, instructions string, criteria map[string]string) Question {
	return Question{
		Name:         name,
		Type:         "choice",
		Instructions: instructions,
		Criteria:     criteria,
	}
}

func NewQueue() *queue {
	return &queue{}
}

type queue struct {
	value Queue
}

func (item *queue) Name(name string) *queue {
	item.value.Name = name
	return item
}

func (item *queue) Skills(skills ...string) *queue {
	item.value.Search.Skills = skills
	return item
}

func (item *queue) Categories(categories ...string) *queue {
	item.value.Search.Categories = categories
	return item
}

func (item *queue) Terms(terms ...string) *queue {
	item.value.Search.Terms = terms
	return item
}

func (item *queue) Regions(regions ...string) *queue {
	item.value.Search.Regions = regions
	return item
}

// Countries are ISO 3166 alpha-2, lowercased by the source that reads them.
func (item *queue) Countries(countries ...string) *queue {
	item.value.Search.Countries = countries
	return item
}

func (item *queue) Location(location ...string) *queue {
	item.value.Search.Location = location
	return item
}

func (item *queue) WorkMode(mode WorkMode) *queue {
	item.value.Search.WorkMode = mode
	return item
}

func (item *queue) PostedWithinDays(days int) *queue {
	item.value.Search.PostedWithinDays = days
	return item
}

func (item *queue) Criteria(criteria Criteria) *queue {
	item.value.Criteria = criteria
	return item
}

func (item *queue) ChatIDs(chatIDs ...string) Queue {
	item.value.ChatIDs = chatIDs
	return item.value
}

var (
	RemoteGlobal = Choice("remote_global",
		"Does this job allow remote work from anywhere in the world, such that a candidate based in Nigeria could do it? Judge the entire posting — its location field as well as its description — not the description alone.",
		map[string]string{
			"yes": "The posting allows remote work worldwide or from anywhere, and the location field does not name a country or region that excludes Nigeria.",
			"no":  "Everything else. The posting restricts the role to a specific country or region; it is on-site or hybrid only; or it does not say which countries it is open to, so eligibility cannot be established. A location field naming a country or city is a hard constraint: \"Lviv, Ukraine\" is no even when the word \"Remote\" appears elsewhere in the posting, and a city or country in a location field always outweighs the description's wording.",
		})

	LocalRole = Choice("local_role",
		"Could a candidate based in Lagos, Nigeria take this job without moving abroad? Judge the entire posting — its location field as well as its description — not the description alone.",
		map[string]string{
			"yes": "The role is in Nigeria, or the employer is Nigeria-based and would hire the candidate there. On-site, hybrid and remote all qualify, as does a location field naming Lagos or Nigeria.",
			"no":  "Everything else. The role is in another country; it is remote but restricted to a region that excludes Nigeria; or it does not say where it is, so eligibility cannot be established.",
		})

	RelocationSupported = Choice("relocation_supported",
		"Does this employer offer relocation support or visa sponsorship for a candidate currently based in Nigeria? Judge the entire posting, not the description alone.",
		map[string]string{
			"yes": "The posting explicitly offers relocation assistance or visa sponsorship.",
			"no":  "Everything else. The posting does not offer relocation or sponsorship, requires existing work authorization, is an on-site role, or never mentions relocation at all, so support cannot be inferred.",
		})
)
