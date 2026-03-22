package main

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// ── Vector search ─────────────────────────────────────────────────────────────

// searchVector performs semantic search using embeddings.
// If no embeddings exist in DB yet, falls back to searchDB transparently.
func searchVector(ctx context.Context, q string, page, perPage int) ([]Command, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM commands WHERE embedding IS NOT NULL").Scan(&count); err != nil || count == 0 {
		log.Printf("vector search: no embeddings in DB, falling back to SQL")
		return searchDB(ctx, q, page, perPage)
	}

	embedding, err := getEmbedding(ctx, q)
	if err != nil {
		log.Printf("vector search failed, falling back to SQL: %v", err)
		return searchDB(ctx, q, page, perPage)
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
	// Fix: include 1 - (embedding <=> $1) as Score so threshold check works
	return queryDBContext(ctx, `
		SELECT t.name, c.syntax, c.description,
		       1 - (c.embedding <=> $1::vector) AS score
		FROM commands c
		JOIN tools t ON c.tool_id = t.id
		WHERE c.embedding IS NOT NULL
		ORDER BY c.embedding <=> $1::vector
		LIMIT $2 OFFSET $3`,
		sb.String(), perPage, offset,
	)
}

// ── FTS: tool + keyword ───────────────────────────────────────────────────────

func searchByToolAndKeyword(ctx context.Context, tool, keyword string, page, perPage int) ([]Command, error) {
	offset := (page - 1) * perPage
	return queryDBContext(ctx, `
		SELECT t.name, c.syntax, c.description, 0 AS score
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

func searchDB(ctx context.Context, term string, page, perPage int) ([]Command, error) {
	var toolExists bool
	if err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM tools WHERE slug ILIKE $1 OR name ILIKE $1)", term,
	).Scan(&toolExists); err != nil {
		return nil, err
	}

	offset := (page - 1) * perPage

	if toolExists {
		return queryDBContext(ctx, `
			SELECT t.name, c.syntax, c.description, 0 AS score
			FROM commands c
			JOIN tools t ON c.tool_id = t.id
			WHERE t.slug ILIKE $1 OR t.name ILIKE $1
			ORDER BY c.syntax
			LIMIT $2 OFFSET $3`,
			term, perPage, offset,
		)
	}

	return queryDBContext(ctx, `
		SELECT t.name, c.syntax, c.description, 0 AS score
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

// ── Raw query executor with context ──────────────────────────────────────────

func queryDBContext(ctx context.Context, sql string, args ...interface{}) ([]Command, error) {
	rows, err := db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Command
	for rows.Next() {
		var cmd Command
		if err := rows.Scan(&cmd.Tool, &cmd.Syntax, &cmd.Description, &cmd.Score); err != nil {
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
