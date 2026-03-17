package main

import (
	"fmt"
	"log"
	"strings"
)

// ── Vector search ─────────────────────────────────────────────────────────────

func searchVector(q string, page, perPage int) ([]Command, error) {
	embedding, err := getEmbedding(q)
	if err != nil {
		log.Printf("vector search failed, falling back to SQL: %v", err)
		return searchDB(q, page, perPage)
	}

	// Convert []float32 → postgres vector string [0.1,0.2,...]
	sb := strings.Builder{}
	sb.WriteString("[")
	for i, v := range embedding {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "%f", v)
	}
	sb.WriteString("]")

	offset := (page - 1) * perPage
	return queryDB(`
		SELECT t.name, c.syntax, c.description
		FROM commands c
		JOIN tools t ON c.tool_id = t.id
		WHERE c.embedding IS NOT NULL
		ORDER BY c.embedding <=> $1::vector
		LIMIT $2 OFFSET $3`,
		sb.String(), perPage, offset,
	)
}

// ── FTS: tool + keyword ───────────────────────────────────────────────────────

func searchByToolAndKeyword(tool, keyword string, page, perPage int) ([]Command, error) {
	offset := (page - 1) * perPage
	return queryDB(`
		SELECT t.name, c.syntax, c.description
		FROM commands c
		JOIN tools t ON c.tool_id = t.id
		WHERE
			(t.slug ILIKE $1 OR t.name ILIKE $1)
			AND to_tsvector('english', c.syntax || ' ' || c.description)
				@@ plainto_tsquery('english', $2)
		ORDER BY
			CASE
				WHEN c.syntax ILIKE $2 || '%' THEN 0
				WHEN c.syntax ILIKE '%' || $2 || '%' THEN 1
				ELSE 2
			END, c.syntax
		LIMIT $3 OFFSET $4`,
		tool, keyword, perPage, offset,
	)
}

// ── General SQL search ────────────────────────────────────────────────────────

func searchDB(term string, page, perPage int) ([]Command, error) {
	var toolExists bool
	if err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM tools WHERE slug ILIKE $1 OR name ILIKE $1)", term,
	).Scan(&toolExists); err != nil {
		return nil, err
	}

	offset := (page - 1) * perPage

	if toolExists {
		return queryDB(`
			SELECT t.name, c.syntax, c.description
			FROM commands c
			JOIN tools t ON c.tool_id = t.id
			WHERE t.slug ILIKE $1 OR t.name ILIKE $1
			ORDER BY c.syntax
			LIMIT $2 OFFSET $3`,
			term, perPage, offset,
		)
	}

	return queryDB(`
		SELECT t.name, c.syntax, c.description
		FROM commands c
		JOIN tools t ON c.tool_id = t.id
		WHERE
			c.syntax ILIKE '%' || $1 || '%'
			OR c.description ILIKE '%' || $1 || '%'
		ORDER BY
			CASE
				WHEN c.syntax ILIKE $1 THEN 0
				WHEN c.syntax ILIKE $1 || '%' THEN 1
				WHEN c.syntax ILIKE '%' || $1 || '%' THEN 2
				WHEN c.description ILIKE '%' || $1 || '%' THEN 3
				ELSE 4
			END, t.name, c.syntax
		LIMIT $2 OFFSET $3`,
		term, perPage, offset,
	)
}

// ── Raw query executor ────────────────────────────────────────────────────────

func queryDB(sql string, args ...interface{}) ([]Command, error) {
	rows, err := db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Command
	for rows.Next() {
		var cmd Command
		if err := rows.Scan(&cmd.Tool, &cmd.Syntax, &cmd.Description); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		results = append(results, cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil {
		results = []Command{}
	}
	return results, nil
}
