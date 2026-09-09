// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package models

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"

	"github.com/jaswdr/faker/v2"
	"xorm.io/builder"
)

func initBenchmarkConfig() {
	if os.Getenv("VIKUNJA_TESTS_USE_CONFIG") == "1" {
		config.InitConfig()
	} else {
		config.InitDefaultConfig()
		config.ServiceRootpath.Set(os.Getenv("VIKUNJA_SERVICE_ROOTPATH"))
	}
}

// createBenchmarkData creates projects and tasks used for search benchmarks.
func createBenchmarkData(b *testing.B, needle string) *user.User {

	numberOfProjects := 10
	numberOfTasks := 2500

	s := db.NewSession()
	defer s.Close()

	f := faker.New()

	u, err := user.GetUserByID(s, 1)
	if err != nil {
		b.Fatalf("get user: %v", err)
	}

	for i := range numberOfProjects {
		p := &Project{Title: fmt.Sprintf("Project %d", i), OwnerID: u.ID}
		if _, err := s.Insert(p); err != nil {
			b.Fatalf("insert project: %v", err)
		}

		for j := range numberOfTasks {
			title := f.Lorem().Sentence(6)
			if rand.Intn(100) == 0 { //nolint:gosec
				title += " " + needle
			}
			desc := ""
			if j%2 == 0 {
				desc = f.Lorem().Paragraph(1)
			}
			if j%100 == 0 {
				if desc == "" {
					desc = f.Lorem().Paragraph(1)
				}
				words := strings.Split(desc, " ")
				mid := len(words) / 2
				words = append(words[:mid], append([]string{needle}, words[mid:]...)...)
				desc = strings.Join(words, " ")
			}
			t := &Task{
				Title:       title,
				Description: desc,
				ProjectID:   p.ID,
				CreatedByID: u.ID,
				Index:       int64(j + 1),
			}
			if _, err := s.Insert(t); err != nil {
				b.Fatalf("insert task: %v", err)
			}
		}
	}

	// The benchmark session holds a transaction; without a commit the projects
	// and tasks are invisible to the query sessions below.
	if err := s.Commit(); err != nil {
		b.Fatalf("commit benchmark data: %v", err)
	}

	return u
}

func BenchmarkTaskSearch(b *testing.B) {
	const needle = "llama"

	initBenchmarkConfig()
	SetupTests()
	err := db.LoadFixtures()
	if err != nil {
		b.Fatalf("load fixtures: %v", err)
	}

	// Log database configuration
	b.Logf("Database Type: %s", config.DatabaseType.GetString())

	auth := createBenchmarkData(b, needle)

	// Get all projects for the user
	s := db.NewSession()
	projects, _, _, err := getRawProjectsForUser(
		s,
		&projectOptions{
			user: auth,
			page: -1,
		},
	)
	s.Close()
	if err != nil {
		b.Fatalf("get projects: %v", err)
	}

	// Create search options
	opts := &taskSearchOptions{
		search:             needle,
		page:               1,
		perPage:            50,
		filter:             "done = false",
		filterTimezone:     "UTC",
		filterIncludeNulls: false,
	}

	b.Log("Setup done, starting benchmark...")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := db.NewSession()
		resultSlice, _, _, err := getRawTasksForProjects(s, projects, auth, opts)
		if len(resultSlice) == 0 {
			b.Fatalf("no results found for needle %q", needle)
		}
		s.Close()
		if err != nil {
			b.Fatalf("search error: %v", err)
		}
	}
}

// createCustomFieldBenchmarkData adds a number definition to the first project
// and a value row for every other task, so filter and sort queries exercise the
// custom_field_values join and subquery at scale.
func createCustomFieldBenchmarkData(b *testing.B, projectID int64) {
	s := db.NewSession()
	defer s.Close()

	def := &CustomFieldDefinition{
		ProjectID:  projectID,
		MachineKey: "benchmark_score",
		Title:      "Benchmark Score",
		FieldType:  CustomFieldTypeNumber,
	}
	if _, err := s.Insert(def); err != nil {
		b.Fatalf("insert definition: %v", err)
	}

	tasks := []*Task{}
	if err := s.Where(builder.Eq{"project_id": projectID}).Find(&tasks); err != nil {
		b.Fatalf("load tasks: %v", err)
	}
	for i, task := range tasks {
		if i%2 != 0 {
			continue
		}
		value := int64(i % 1000)
		if _, err := s.Insert(&TaskCustomFieldValue{TaskID: task.ID, DefinitionID: def.ID, ValueNumber: &value}); err != nil {
			b.Fatalf("insert value: %v", err)
		}
	}
	if err := s.Commit(); err != nil {
		b.Fatalf("commit benchmark data: %v", err)
	}
}

func BenchmarkCustomFieldFilter(b *testing.B) {
	initBenchmarkConfig()
	SetupTests()
	err := db.LoadFixtures()
	if err != nil {
		b.Fatalf("load fixtures: %v", err)
	}

	auth := createBenchmarkData(b, "llama")

	s := db.NewSession()
	projects, _, _, err := getRawProjectsForUser(
		s,
		&projectOptions{
			user: auth,
			page: -1,
		},
	)
	s.Close()
	if err != nil {
		b.Fatalf("get projects: %v", err)
	}

	// Host the definition and values in a benchmark project (created by
	// createBenchmarkData with 2500 tasks), not a fixture project, so the
	// filter and sort exercise the value table at scale. The benchmark projects
	// are titled "Project 0".."Project 9"; the fixture projects use longer
	// titles like "Project 36 for Caldav tests".
	s = db.NewSession()
	benchmarkProjects := []*Project{}
	if err := s.Where(&builder.Like{"title", "Project _"}).Find(&benchmarkProjects); err != nil {
		b.Fatalf("find benchmark projects: %v", err)
	}
	s.Close()
	if len(benchmarkProjects) == 0 {
		b.Fatalf("no benchmark projects found")
	}
	createCustomFieldBenchmarkData(b, benchmarkProjects[0].ID)

	// Re-resolve the project list so the definition is visible.
	s = db.NewSession()
	projects, _, _, err = getRawProjectsForUser(
		s,
		&projectOptions{
			user: auth,
			page: -1,
		},
	)
	s.Close()
	if err != nil {
		b.Fatalf("get projects: %v", err)
	}

	filterOpts := &taskSearchOptions{
		page:           1,
		perPage:        50,
		filter:         "custom_fields.benchmark_score > 500",
		filterTimezone: "UTC",
	}
	// Resolution mutates the parsed filters (casts values, attaches definitions),
	// so keep a pristine copy and clone it per iteration.
	pristineFilters, err := getTaskFiltersFromFilterString(filterOpts.filter, filterOpts.filterTimezone, true)
	if err != nil {
		b.Fatalf("parse filter: %v", err)
	}
	sortOpts := &taskSearchOptions{
		page:    1,
		perPage: 50,
		sortby: []*sortParam{{
			sortBy:  "custom_fields.benchmark_score",
			orderBy: orderDescending,
		}},
	}

	b.Log("Setup done, starting benchmark...")

	b.Run("filter", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			s := db.NewSession()
			opts := *filterOpts
			opts.parsedFilters = cloneTaskFilters(pristineFilters)
			resultSlice, _, _, err := getRawTasksForProjects(s, projects, auth, &opts)
			if len(resultSlice) == 0 {
				b.Fatalf("no results found for custom-field filter")
			}
			s.Close()
			if err != nil {
				b.Fatalf("filter error: %v", err)
			}
		}
	})

	b.Run("sort", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			s := db.NewSession()
			resultSlice, _, _, err := getRawTasksForProjects(s, projects, auth, sortOpts)
			if len(resultSlice) == 0 {
				b.Fatalf("no results found for custom-field sort")
			}
			s.Close()
			if err != nil {
				b.Fatalf("sort error: %v", err)
			}
		}
	})
}
