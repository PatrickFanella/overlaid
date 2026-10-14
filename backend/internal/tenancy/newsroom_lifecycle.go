package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// This works on the retained JSON document inside the lifecycle transaction.
// It does not initialize a product runtime or decrypt provider credentials.
func pruneNewsroom(ctx context.Context, tx *tenantdb.Tx, viewerID, login string, cutoff *time.Time) error {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT document FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND id='default' FOR UPDATE`).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err = json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	str := func(row map[string]json.RawMessage, key string) string {
		var s string
		_ = json.Unmarshal(row[key], &s)
		return s
	}
	matches := func(row map[string]json.RawMessage) bool {
		return viewerID != "" && (str(row, "viewerId") == viewerID || login != "" && (strings.EqualFold(str(row, "submittedBy"), login) || strings.EqualFold(str(row, "askedBy"), login) || strings.EqualFold(str(row, "login"), login)))
	}
	changed := false
	removed := map[string]bool{}
	affectedShows := map[string]bool{}
	for _, key := range []string{"items", "questions", "history", "bundles", "activity"} {
		var rows []map[string]json.RawMessage
		if err = json.Unmarshal(doc[key], &rows); err != nil && len(doc[key]) > 0 {
			return err
		}
		kept := []map[string]json.RawMessage{}
		for _, row := range rows {
			drop := matches(row)
			status := str(row, "status")
			if cutoff != nil && (status == "played" || status == "rejected" || status == "answered" || key == "history" || key == "activity" || key == "bundles" && str(row, "leadId") == "") {
				var at time.Time
				date := row["createdAt"]
				if key == "history" || key == "activity" {
					date = row["at"]
				}
				if json.Unmarshal(date, &at) == nil && at.Before(*cutoff) {
					drop = true
				}
			}
			if drop {
				changed = true
				removed[str(row, "id")] = true
				if key == "history" && viewerID != "" {
					affectedShows[str(row, "broadcastId")] = true
					removed[str(row, "itemId")] = true
				}
			} else {
				kept = append(kept, row)
			}
		}
		doc[key], _ = json.Marshal(kept)
	}
	if viewerID != "" {
		var raffle map[string]json.RawMessage
		if err = json.Unmarshal(doc["raffle"], &raffle); err != nil && len(doc["raffle"]) > 0 {
			return err
		}
		if raffle != nil {
			var entries []map[string]json.RawMessage
			_ = json.Unmarshal(raffle["entries"], &entries)
			kept := []map[string]json.RawMessage{}
			for _, entry := range entries {
				if matches(entry) {
					changed = true
				} else {
					kept = append(kept, entry)
				}
			}
			raffle["entries"], _ = json.Marshal(kept)
			if login != "" && strings.EqualFold(str(raffle, "winner"), login) {
				raffle["winner"] = json.RawMessage(`""`)
				changed = true
			}
			doc["raffle"], _ = json.Marshal(raffle)
		}
	}
	var shows []map[string]json.RawMessage
	if err = json.Unmarshal(doc["broadcasts"], &shows); err != nil && len(doc["broadcasts"]) > 0 {
		return err
	}
	keptShows := []map[string]json.RawMessage{}
	for _, show := range shows {
		var ended time.Time
		if cutoff != nil && json.Unmarshal(show["endedAt"], &ended) == nil && ended.Before(*cutoff) {
			changed = true
			affectedShows[str(show, "id")] = true
			continue
		}
		if viewerID != "" {
			var rundown []map[string]json.RawMessage
			if err = json.Unmarshal(show["rundown"], &rundown); err != nil && len(show["rundown"]) > 0 {
				return err
			}
			kept := []map[string]json.RawMessage{}
			for _, row := range rundown {
				if matches(row) || removed[str(row, "id")] {
					affectedShows[str(show, "id")] = true
					changed = true
				} else {
					kept = append(kept, row)
				}
			}
			show["rundown"], _ = json.Marshal(kept)
		}
		keptShows = append(keptShows, show)
	}
	doc["broadcasts"], _ = json.Marshal(keptShows)
	var exports []map[string]json.RawMessage
	_ = json.Unmarshal(doc["showExports"], &exports)
	keptExports := []map[string]json.RawMessage{}
	for _, job := range exports {
		if !affectedShows[str(job, "showId")] {
			keptExports = append(keptExports, job)
		}
	}
	doc["showExports"], _ = json.Marshal(keptExports)
	var publications map[string]json.RawMessage
	_ = json.Unmarshal(doc["publications"], &publications)
	for show := range affectedShows {
		delete(publications, show)
		if _, err = tx.ExecContext(ctx, `UPDATE newsroom_documents SET token_hash=NULL WHERE /*tenant:newsroom_documents*/ AND id=$1`, "episode:"+show); err != nil {
			return err
		}
	}
	doc["publications"], _ = json.Marshal(publications)

	if !changed {
		return nil
	}
	// Retention can remove a lead without removing its still useful bundle.
	var bundles []map[string]json.RawMessage
	_ = json.Unmarshal(doc["bundles"], &bundles)
	for _, bundle := range bundles {
		if removed[str(bundle, "leadId")] {
			bundle["leadId"] = json.RawMessage(`""`)
		}
	}
	doc["bundles"], _ = json.Marshal(bundles)
	var order []string
	_ = json.Unmarshal(doc["order"], &order)
	keptOrder := []string{}
	for _, id := range order {
		if !removed[id] {
			keptOrder = append(keptOrder, id)
		}
	}
	doc["order"], _ = json.Marshal(keptOrder)
	for _, key := range []string{"currentId", "currentQuestion"} {
		var id string
		_ = json.Unmarshal(doc[key], &id)
		if removed[id] {
			doc[key] = json.RawMessage(`""`)
		}
	}
	var revision int64
	_ = json.Unmarshal(doc["revision"], &revision)
	doc["revision"], _ = json.Marshal(revision + 1)
	raw, err = json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE newsroom_documents SET document=$1,updated_at=now() WHERE /*tenant:newsroom_documents*/ AND id='default'`, raw)
	return err
}
