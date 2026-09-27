package medication

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type Drug struct {
	Name   string `yaml:"name"`
	Dose   string `yaml:"dose"`
	Amount string `yaml:"amount"`
}
type Reminder struct {
	ID          string `yaml:"id"`
	Time        string `yaml:"time"`
	Medications []Drug `yaml:"medications"`
}
type Config struct {
	Timezone  string         `yaml:"timezone"`
	Reminders []Reminder     `yaml:"reminders"`
	Location  *time.Location `yaml:"-"`
}

var clock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var c Config
	d := yaml.NewDecoder(f)
	d.KnownFields(true)
	if err = d.Decode(&c); err != nil {
		return nil, err
	}
	c.Location, err = time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, fmt.Errorf("timezone: %w", err)
	}
	if len(c.Reminders) == 0 {
		return nil, fmt.Errorf("empty reminders")
	}
	ids := map[string]bool{}
	for _, r := range c.Reminders {
		if r.ID == "" || strings.Contains(r.ID, ":") || ids[r.ID] || !clock.MatchString(r.Time) || len(r.Medications) == 0 {
			return nil, fmt.Errorf("invalid reminder %q", r.ID)
		}
		ids[r.ID] = true
		for _, m := range r.Medications {
			if m.Name == "" || m.Dose == "" || m.Amount == "" {
				return nil, fmt.Errorf("incomplete medication in %s", r.ID)
			}
		}
	}
	return &c, nil
}

// A schedule day begins at the first listed daytime dose. Times earlier than
// that dose belong to the following civil date (e.g. 01:30 after 20:00).
func (c *Config) Scheduled(day time.Time, r Reminder) time.Time {
	h := int(r.Time[0]-'0')*10 + int(r.Time[1]-'0')
	m := int(r.Time[3]-'0')*10 + int(r.Time[4]-'0')
	y, mo, d := day.In(c.Location).Date()
	t := time.Date(y, mo, d, h, m, 0, 0, c.Location)
	first := c.Reminders[0].Time
	if r.Time < first {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func (c *Config) CurrentDay(now time.Time) time.Time {
	local := now.In(c.Location)
	y, mo, d := local.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, c.Location)
	if local.Before(c.Scheduled(today, c.Reminders[0])) {
		return today.AddDate(0, 0, -1)
	}
	return today
}

func (c *Config) Find(id string) (Reminder, bool) {
	for _, r := range c.Reminders {
		if r.ID == id {
			return r, true
		}
	}
	return Reminder{}, false
}
func Names(r Reminder) string {
	a := make([]string, 0, len(r.Medications))
	for _, m := range r.Medications {
		a = append(a, m.Name)
	}
	return strings.Join(a, " + ")
}
func Details(r Reminder) string {
	a := make([]string, 0, len(r.Medications))
	for _, m := range r.Medications {
		a = append(a, fmt.Sprintf("%s %s — %s", m.Name, m.Dose, m.Amount))
	}
	return strings.Join(a, "\n")
}
