package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/changez/changez/internal/db"
)

func main() {
	d, err := db.Open("/home/lan/workspace/go/changez/data/changez.db")
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	projects, err := d.ListProjects(context.Background())
	fmt.Printf("Projects: %d\n", len(projects))
	for _, p := range projects {
		fmt.Printf("  %v\n", p)
	}

	rows, err := d.Query(context.Background(), "SELECT id, path, project_id, source_id FROM files ORDER BY id DESC LIMIT 20")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nFiles (last 20):\n")
	for rows.Next() {
		var id, projectID, sourceID int64
		var fp string
		rows.Scan(&id, &fp, &projectID, &sourceID)
		fmt.Printf("  [%d] %s | project=%d | source=%d\n", id, fp, projectID, sourceID)
	}
	rows.Close()

	rows2, err := d.Query(context.Background(), "SELECT id, file_id, source_id, version_index, created_at FROM versions ORDER BY id DESC LIMIT 10")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nVersions (last 10):\n")
	for rows2.Next() {
		var id, fileID, sourceID, vindex int64
		var created string
		rows2.Scan(&id, &fileID, &sourceID, &vindex, &created)
		fmt.Printf("  [%d] file=%d | source=%d | index=%d | %s\n", id, fileID, sourceID, vindex, created)
	}
	rows2.Close()

	var opencodeCount int
	err = d.Handle().QueryRow("SELECT COUNT(*) FROM versions WHERE source_id = 1").Scan(&opencodeCount)
	fmt.Printf("\nVersions from opencode (source_id=1): %d\n", opencodeCount)

	var recentCount int
	err = d.Handle().QueryRow("SELECT COUNT(*) FROM versions WHERE created_at > datetime('now', '-24 hours')").Scan(&recentCount)
	fmt.Printf("Versions in last 24h: %d\n", recentCount)
	_ = sql.ErrNoRows
}
