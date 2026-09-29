#!/usr/bin/env python3
"""Regenerate tests/fixtures/opencode-home/opencode.db.

Schema mirrors opencode 1.18.x (baseline 1.18.33): the `session` table drives
metadata/tokens, conversations are dual-projected into `session_message`
(durable pipeline) and the legacy `message`+`part` pair (v1-compat layer).
The fixture exercises: dual-write dedupe, pre-upgrade sessions that exist only
in the legacy pair, archived/child skipping, an oversized overflow-page blob,
and (via storage/) the pre-1.1 JSON layout fallback.

Usage: python3 gen_fixture.py opencode.db
"""
import json
import sqlite3
import sys

MS = 1000  # epoch millis helper

# Fixed epoch-ms anchors (all 2026-04-15 UTC):
T = {
    "aaaa_created": 1776229200000,  # 05:00:00
    "aaaa_p1": 1776229200000,       # 05:00:00
    "aaaa_a1": 1776229205000,       # 05:00:05
    "aaaa_p2": 1776229300000,       # 05:01:40
    "aaaa_a2": 1776229400000,       # 05:03:20
    "aaaa_updated": 1776229500000,  # 05:05:00
    "bbbb_created": 1776228000000,  # 04:40:00
    "bbbb_updated": 1776229000000,  # 04:56:40
    "bbbb_archived": 1776230000000, # 05:13:20
    "cccc_created": 1776229600000,  # 05:06:40
    "cccc_updated": 1776229700000,  # 05:08:20
    "dddd_created": 1776234000000,  # 06:20:00
    "dddd_p1": 1776234000000,       # 06:20:00
    "dddd_a1": 1776234050000,       # 06:20:50
    "dddd_updated": 1776234100000,  # 06:21:40
    "eeee_created": 1776232800000,  # 06:00:00
    "eeee_p1": 1776232800000,       # 06:00:00
    "eeee_a1": 1776232900000,       # 06:01:40
    "eeee_p2": 1776233000000,       # 06:03:20
    "eeee_updated": 1776233100000,  # 06:05:00
}

SES = {
    "aaaa": "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "bbbb": "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "cccc": "ses_cccccccccccccccccccccccccccc",
    "dddd": "ses_dddddddddddddddddddddddddddd",
    "eeee": "ses_eeeeeeeeeeeeeeeeeeeeeeeeeeee",
}

SCHEMA = """
CREATE TABLE session (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, workspace_id TEXT,
  parent_id TEXT, slug TEXT NOT NULL, directory TEXT NOT NULL, path TEXT,
  title TEXT NOT NULL, version TEXT NOT NULL, share_url TEXT,
  summary_additions INTEGER, summary_deletions INTEGER, summary_files INTEGER,
  summary_diffs TEXT, metadata TEXT, cost REAL NOT NULL DEFAULT 0,
  tokens_input INTEGER NOT NULL DEFAULT 0, tokens_output INTEGER NOT NULL DEFAULT 0,
  tokens_reasoning INTEGER NOT NULL DEFAULT 0, tokens_cache_read INTEGER NOT NULL DEFAULT 0,
  tokens_cache_write INTEGER NOT NULL DEFAULT 0, revert TEXT, permission TEXT,
  agent TEXT, model TEXT, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL,
  time_compacting INTEGER, time_archived INTEGER
);
CREATE TABLE session_message (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, type TEXT NOT NULL,
  seq INTEGER NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL,
  data TEXT NOT NULL, UNIQUE (session_id, seq)
);
CREATE TABLE message (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL
);
CREATE TABLE part (
  id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL
);
"""

def session_row(sid, project, slug, directory, title, created, updated,
                tokens=(0, 0, 0, 0, 0), parent=None, archived=None, diffs=None):
    return (sid, project, None, parent, slug, directory, None, title, "1.18.33",
            None, None, None, None, diffs, None, 0.0,
            tokens[0], tokens[1], 0, tokens[2], tokens[3],
            None, None, None, None, created, updated, None, archived)

def main(path):
    db = sqlite3.connect(path)
    db.execute("PRAGMA journal_mode=WAL")
    db.executescript(SCHEMA)

    # --- sessions ---------------------------------------------------------
    # ses_aaaa: healthy dual-projected session (both table families hold the
    # same two turns — resumer must dedupe, not double-count).
    db.execute("INSERT INTO session VALUES (%s)" % ",".join("?" * 29),
               session_row(SES["aaaa"], "proj-oc", "one", "/tmp/resumer-fixtures/oc-one",
                           "OpenCode Fixture One", T["aaaa_created"], T["aaaa_updated"],
                           tokens=(1500, 300, 9000, 1200)))
    # ses_bbbb: archived → skipped. Carries a >4KiB summary_diffs blob so the
    # reader must follow an overflow page chain.
    big = json.dumps([{"file": "big%d.txt" % i, "additions": i} for i in range(400)])
    assert len(big) > 8192
    db.execute("INSERT INTO session VALUES (%s)" % ",".join("?" * 29),
               session_row(SES["bbbb"], "proj-oc", "two", "/tmp/resumer-fixtures/oc-two",
                           "OpenCode Archived Fixture", T["bbbb_created"], T["bbbb_updated"],
                           archived=T["bbbb_archived"], diffs=big))
    # ses_cccc: subagent child of ses_aaaa → skipped (parent_id set).
    db.execute("INSERT INTO session VALUES (%s)" % ",".join("?" * 29),
               session_row(SES["cccc"], "proj-oc", "one", "/tmp/resumer-fixtures/oc-one",
                           "OpenCode Child Fixture", T["cccc_created"], T["cccc_updated"],
                           parent=SES["aaaa"]))
    # ses_dddd: durable-projection-only session (newest → integration test target).
    db.execute("INSERT INTO session VALUES (%s)" % ",".join("?" * 29),
               session_row(SES["dddd"], "proj-oc", "three", "/tmp/resumer-fixtures/oc-three",
                           "OpenCode Fixture Three", T["dddd_created"], T["dddd_updated"]))
    # ses_eeee: pre-upgrade session — exists ONLY in the legacy message+part
    # pair (no session_message rows, mirroring the missing backfill).
    db.execute("INSERT INTO session VALUES (%s)" % ",".join("?" * 29),
               session_row(SES["eeee"], "proj-oc", "two", "/tmp/resumer-fixtures/oc-two",
                           "OpenCode Legacy Session", T["eeee_created"], T["eeee_updated"]))

    # --- session_message (durable projection) ------------------------------
    sm = [
        ("msg_aa1", SES["aaaa"], "user", 1, T["aaaa_p1"], "user",
         "opencode fixture one first prompt"),
        ("msg_aa2", SES["aaaa"], "assistant", 2, T["aaaa_a1"], "assistant", ""),
        ("msg_aa3", SES["aaaa"], "user", 3, T["aaaa_p2"], "user",
         "opencode fixture one second prompt"),
        ("msg_aa4", SES["aaaa"], "assistant", 4, T["aaaa_a2"], "assistant", ""),
        ("msg_dd1", SES["dddd"], "user", 1, T["dddd_p1"], "user",
         "opencode fixture three prompt"),
        ("msg_dd2", SES["dddd"], "assistant", 2, T["dddd_a1"], "assistant", ""),
    ]
    for i, (mid, sid, typ, seq, ts, dtyp, text) in enumerate(sm):
        db.execute("INSERT INTO session_message VALUES (?,?,?,?,?,?,?)",
                   (mid, sid, typ, seq, ts, ts + 1000,
                    json.dumps({"type": dtyp, "text": text})))

    # --- legacy message+part ------------------------------------------------
    # Dual-write duplicates of ses_aaaa's turns: same timestamps, so resumer
    # must collapse them. msg_j1 additionally carries an assistant text part
    # that must never surface as a prompt.
    legacy = [
        # (msg id, ses, role, ts, [(part id, part type, text, synthetic)])
        ("msg_j3", SES["aaaa"], "user", T["aaaa_p1"],
         [("part_j3", "text", "opencode fixture one first prompt", False)]),
        ("msg_j1", SES["aaaa"], "assistant", T["aaaa_a1"],
         [("part_j1", "text", "legacy part prompt that must not leak", False)]),
        ("msg_j4", SES["aaaa"], "user", T["aaaa_p2"],
         [("part_j4", "text", "opencode fixture one second prompt", False)]),
        ("msg_j2", SES["aaaa"], "assistant", T["aaaa_a2"],
         [("part_j2", "text", "fixture one second reply", False)]),
        # ses_eeee: legacy-only conversation, incl. a synthetic part decoy.
        ("msg_e1", SES["eeee"], "user", T["eeee_p1"],
         [("part_e1", "text", "legacy only first prompt", False)]),
        ("msg_e2", SES["eeee"], "assistant", T["eeee_a1"],
         [("part_e2", "text", "legacy only reply", False)]),
        # Multiple assistant messages can form one user turn (e.g. tool call
        # continuations); parentID links them back to the same user message.
        ("msg_e2b", SES["eeee"], "assistant", T["eeee_a1"] + 1000,
         [("part_e2b", "text", "legacy only tool continuation", False)]),
        ("msg_e3", SES["eeee"], "user", T["eeee_p2"],
         [("part_e3", "text", "legacy only second prompt", False),
          ("part_e4", "text", "synthetic legacy prompt that must not leak", True)]),
    ]
    for mid, sid, role, ts, parts in legacy:
        message_data = {"role": role}
        if mid in ("msg_e2", "msg_e2b"):
            message_data["parentID"] = "msg_e1"
        db.execute("INSERT INTO message VALUES (?,?,?,?,?)",
                   (mid, sid, ts, ts + 1000, json.dumps(message_data)))
        for pid, ptyp, ptext, synthetic in parts:
            db.execute("INSERT INTO part VALUES (?,?,?,?,?,?)",
                       (pid, mid, sid, ts, ts + 1000,
                        json.dumps({"type": ptyp, "text": ptext, "synthetic": synthetic})))

    db.commit()
    db.execute("PRAGMA wal_checkpoint(TRUNCATE)")
    db.close()
    print("wrote", path)

if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else "opencode.db")
