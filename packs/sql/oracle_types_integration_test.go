//go:build integration

package sql

import (
	"fmt"
	"strings"
	"testing"
)

// oracleTypesSchema is a table with a column of every type Oracle has, and values at
// the edges of how the driver gives them.
var oracleTypesSchema = []string{
	`CREATE TABLE every_type (
		reference VARCHAR2(40) PRIMARY KEY,
		street VARCHAR2(40), recipient NVARCHAR2(40), depot CHAR(8), zone_code NCHAR(8), note CLOB, n_note NCLOB,
		grams NUMBER(10), tracking NUMBER(38), hundreds NUMBER(5,-2), pieces INTEGER, tries SMALLINT,
		kilos NUMBER(10,2), star_kilos NUMBER(*,2), fee FLOAT, rate DOUBLE PRECISION, ratio REAL, huge FLOAT, tiny FLOAT,
		b_float BINARY_FLOAT, b_double BINARY_DOUBLE, b_whole BINARY_DOUBLE,
		bare_whole NUMBER, bare_decimal NUMBER, bare_big NUMBER, bare_long NUMBER, bare_negative NUMBER, largest FLOAT, smallest FLOAT,
		signed_for BOOLEAN, returned BOOLEAN, no_flag BOOLEAN,
		day DATE, day_at DATE, planned_at TIMESTAMP, planned_ms TIMESTAMP(3), zoned_at TIMESTAMP WITH TIME ZONE, local_at TIMESTAMP WITH LOCAL TIME ZONE,
		span_ym INTERVAL YEAR TO MONTH, span_ds INTERVAL DAY TO SECOND,
		token RAW(16), photo BLOB, details JSON, missing VARCHAR2(10))`,
	`INSERT INTO every_type (reference, street, recipient, depot, zone_code, note, n_note,
		grams, tracking, hundreds, pieces, tries,
		kilos, star_kilos, fee, rate, ratio, huge, tiny,
		b_float, b_double, b_whole,
		bare_whole, bare_decimal, bare_big, bare_long, bare_negative, largest, smallest,
		signed_for, returned,
		day, day_at, planned_at, planned_ms, zoned_at, local_at,
		span_ym, span_ds, token, photo, details) VALUES
		('PX-TYPES-1', 'Müllerstraße', 'Jürgen', 'H91', 'DE1', 'left at the door', 'Jürgen signed',
		-850, 12345678901234567890123456789012345678, 1200, 2, 3,
		0.5, -2.5, 1.5, 0.1, 2, 1E+20, 1E-7,
		0.1, 0.1, 2,
		7, 7.5, 12345678901234567890123, 0.1234567890123456789, -0.25, 1E+125, 1E-125,
		TRUE, FALSE,
		DATE '2026-09-24', TO_DATE('2026-09-24 07:40:00', 'YYYY-MM-DD HH24:MI:SS'),
		TIMESTAMP '2026-09-24 07:40:00.123', TIMESTAMP '2026-09-24 07:40:00.125',
		TIMESTAMP '2026-09-24 07:40:00 +02:00', TIMESTAMP '2026-09-24 07:40:00 +02:00',
		INTERVAL '1-2' YEAR TO MONTH, INTERVAL '1 02:03:04' DAY TO SECOND,
		HEXTORAW('DEADBEEF'), HEXTORAW('DEADBEEF'), JSON('{"zone":"DE-1","count":3}'))`,
	`INSERT INTO every_type (reference, grams, no_flag, missing) VALUES ('ABC-OTHER', 1, TRUE, 'x')`,
	`CREATE VIEW every_type_view AS SELECT * FROM every_type`,
	`CREATE SYNONYM every_type_synonym FOR every_type`,
	`CREATE SYNONYM every_type_again FOR every_type_synonym`,
	`CREATE TABLE trigger_parcels (reference VARCHAR2(40) PRIMARY KEY, status VARCHAR2(20), kilos NUMBER(10,2))`,
	`CREATE TABLE clock_times (reference VARCHAR2(40) PRIMARY KEY, day_at DATE, plain_at TIMESTAMP, zoned_at TIMESTAMP WITH TIME ZONE, local_at TIMESTAMP WITH LOCAL TIME ZONE, epoch NUMBER)`,
	`CREATE TABLE load_parcels (reference VARCHAR2(40) PRIMARY KEY, kilos NUMBER(10,2), at_time DATE)`,
	`CREATE SYNONYM loop_a FOR loop_b`,
	`CREATE SYNONYM loop_b FOR loop_a`,
	`CREATE TABLE old_notes (reference VARCHAR2(40) PRIMARY KEY, body LONG)`,
	`INSERT INTO old_notes VALUES ('PX-TYPES-1', 'left at the door')`,
	`CREATE TABLE old_photos (reference VARCHAR2(40) PRIMARY KEY, photo LONG RAW)`,
	`INSERT INTO old_photos VALUES ('PX-TYPES-1', HEXTORAW('DEADBEEF'))`,
	`CREATE TABLE rare_types (reference VARCHAR2(40) PRIMARY KEY, row_ref ROWID, any_row UROWID, embedding VECTOR(3, FLOAT32), fine VECTOR(2, FLOAT64), small VECTOR(2, INT8), doc XMLTYPE, bits VECTOR(8, BINARY))`,
	`INSERT INTO rare_types (reference, row_ref, any_row, embedding, fine, small, doc, bits) SELECT 'PX-TYPES-1', ROWID, ROWID, TO_VECTOR('[1,2,3]', 3, FLOAT32), TO_VECTOR('[0.1,0.5]', 2, FLOAT64), TO_VECTOR('[1,-2]', 2, INT8), XMLTYPE('<parcel/>'), TO_VECTOR('[254]', 8, BINARY) FROM old_notes`,
	`CREATE TABLE seeded_types AS SELECT * FROM every_type WHERE 1 = 0`,
}

func oracleEveryType(t *testing.T, h *harness, tgt Target, host, port string) {
	// A failure stays its subtest's, where h.ok would end them all.
	ok := func(t *testing.T, step string, rows ...[]string) {
		t.Helper()
		if err := h.step(step, rows...); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
	}
	t.Run("the types a condition cannot compare are read by a column check", func(t *testing.T) {
		for _, c := range []struct {
			table, column, cell, value string
			refused, wrong             string
		}{
			{"old_notes", "body", "left at the door", "left at the door", "ORA-", `body is text in old_notes (LONG): write "7"`},
			{"old_photos", "photo", "DEADBEEF", "DEADBEEF", "ORA-", `photo is written as a string in old_photos (LONG RAW): write "7"`},
			{"rare_types", "embedding", "[1,2,3]", "[1.0,2.0,3.0]", "ORA-", `embedding is written as a string in rare_types (VECTOR(3,FLOAT32,DENSE)): write "7"`},
			{"rare_types", "fine", "[0.1,0.5]", "[0.1,0.5]", "ORA-", `fine is written as a string in rare_types (VECTOR(2,FLOAT64,DENSE)): write "7"`},
			{"rare_types", "small", "[1,-2]", "[1,-2]", "ORA-", `small is written as a string in rare_types (VECTOR(2,INT8,DENSE)): write "7"`},
			{"rare_types", "bits", "[254]", "[254]", "ORA-", `bits is written as a string in rare_types (VECTOR(8,BINARY,DENSE)): write "7"`},
			{"rare_types", "doc", "<parcel/>", "<parcel/>", "ORA-", `doc is written as a string in rare_types (XMLTYPE): write "7"`},
		} {
			sel := "a selection of rows is retrieved from the " + c.table + " table where:"
			if err := h.step(sel, []string{c.column, c.cell}); err == nil || !strings.Contains(err.Error(), c.refused) {
				t.Errorf("%s = %s: %v, want Oracle's refusal", c.column, c.cell, err)
			}
			if err := h.step(sel, []string{c.column, "7"}); err == nil || !strings.Contains(err.Error(), c.wrong) {
				t.Errorf("%s 7: %v, want %s", c.column, err, c.wrong)
			}
			ok(t, sel, []string{"reference", "PX-TYPES-1"})
			n := nth(len(mustService(t, h).Selections()))
			if err := h.step("the 1st row " + c.column + " column of the " + n + " selection is '" + c.value + "'"); err != nil {
				t.Errorf("%s: %v", c.column, err)
			}
			if err := h.step("the 1st row " + c.column + " column of the " + n + " selection is 'something else'"); err == nil {
				t.Errorf("%s matched something else", c.column)
			}
		}
	})

	t.Run("what Oracle itself refuses comes back as its error", func(t *testing.T) {
		for _, c := range [][2]string{{"day", "soon"}, {"planned_at", "soon"}, {"details", "not json"}} {
			err := h.step("a selection of rows is retrieved from the every_type table where:", []string{c[0], c[1]})
			if err == nil || !strings.Contains(err.Error(), "ORA-") {
				t.Errorf("%s %s: %v, want Oracle's error", c[0], c[1], err)
			}
		}
		// A synonym of itself ends, in Oracle's error.
		err := h.step("a selection of rows is retrieved from the loop_a table where:", []string{"grams", "1"})
		if err == nil || !strings.Contains(err.Error(), "ORA-01775") {
			t.Errorf("a synonym of itself: %v", err)
		}
		for _, c := range []struct {
			table, column, want string
		}{
			{"every_type", "no_such_column", "ORA-"},
			{"no_such_table", "grams", "ORA-"},
			{"not-a-table", "grams", "not-a-table"},
			{"every_type", "not-a-column", "not-a-column"},
		} {
			err := h.step("a before insert trigger on the "+c.table+" table will raise an ORA-20001 exception where:", []string{c.column, "1"})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("a trigger on %s.%s: %v, want %s", c.table, c.column, err, c.want)
			}
		}
		if got := len(mustService(t, h).Triggers()); got != 0 {
			t.Errorf("%d triggers were created", got)
		}
	})

	t.Run("a ROWID is the text Oracle gives", func(t *testing.T) {
		ok(t, "a selection of rows is retrieved from the rare_types table where:", []string{"reference", "PX-TYPES-1"})
		svc := mustService(t, h)
		all := svc.Selections()
		id, err := all[len(all)-1].Value(0, "row_ref")
		if err != nil {
			t.Fatal(err)
		}
		n := nth(len(all))
		for _, column := range []string{"row_ref", "any_row"} {
			if err := h.step("the 1st row " + column + " column of the " + n + " selection is '" + text(id) + "'"); err != nil {
				t.Errorf("%s: %v", column, err)
			}
			if err := h.step("a selection of rows is retrieved from the rare_types table where:", []string{column, text(id)}); err != nil {
				t.Errorf("%s: %v", column, err)
				continue
			}
			all := svc.Selections()
			if got := len(all[len(all)-1].Rows); got != 1 {
				t.Errorf("%s selected %d rows", column, got)
			}
			if err := h.step("a selection of rows is retrieved from the rare_types table where:", []string{column, "7"}); err == nil || !strings.Contains(err.Error(), "written as a string") {
				t.Errorf("%s 7: %v", column, err)
			}
		}
	})

	t.Run("every type's values are held to it", func(t *testing.T) {
		columnCases{
			table: "every_type",
			fails: [][3]string{
				{"street", "7", `street is text in every_type (VARCHAR2): write "7"`},
				{"recipient", "true", `recipient is text in every_type (NVARCHAR2): write "true"`},
				{"depot", "12345", `depot is text in every_type (CHAR): write "12345"`},
				{"zone_code", "1", `zone_code is text in every_type (NCHAR): write "1"`},
				{"note", "7", `note is text in every_type (CLOB): write "7"`},
				{"n_note", "true", `n_note is text in every_type (NCLOB): write "true"`},
				{"street", `""`, `Oracle stores an empty string as null`},
				{"depot", `""`, `Oracle stores an empty string as null`},
				{"note", `""`, `Oracle stores an empty string as null`},

				{"grams", "-850.0", `grams is an integer in every_type (NUMBER(10,0)): write -850`},
				{"grams", "-850.5", `grams is an integer in every_type (NUMBER(10,0)): write an integer, not -850.5`},
				{"tracking", `"12345678901234567890123456789012345678"`, `tracking is an integer in every_type (NUMBER(38,0))`},
				{"hundreds", "1200.0", `hundreds is an integer in every_type (NUMBER(5,-2)): write 1200`},
				{"tries", "true", `tries is an integer in every_type (NUMBER(*,0))`},

				{"kilos", "1", `kilos is a decimal in every_type (NUMBER(10,2)): write 1.0`},
				{"kilos", `"0.5"`, `kilos is a decimal in every_type (NUMBER(10,2)): write 0.5`},
				{"star_kilos", "-2", `star_kilos is a decimal in every_type (NUMBER(*,2)): write -2.0`},
				{"rate", "1", `rate is a decimal in every_type (FLOAT)`},
				{"ratio", "2", `ratio is a decimal in every_type (FLOAT): write 2.0`},
				{"b_float", `"0.1"`, `b_float is a decimal in every_type (BINARY_FLOAT): write 0.1`},
				{"b_whole", "2", `b_whole is a decimal in every_type (BINARY_DOUBLE): write 2.0`},

				{"signed_for", "1", `signed_for is a boolean in every_type (BOOLEAN): write true`},
				{"signed_for", `"true"`, `signed_for is a boolean in every_type (BOOLEAN): write true`},
				{"returned", "0", `returned is a boolean in every_type (BOOLEAN): write false`},

				{"day_at", "2026", `day_at is written as a string in every_type (DATE): write "2026"`},
				{"day_at", "2026-09-24T07:40:00+02:00", `day_at has no time zone in every_type (DATE), so an offset would be dropped: write "2026-09-24 07:40:00"`},
				{"day_at", "2026-09-24 07:40:00-05:00", `write "2026-09-24 07:40:00"`},
				{"planned_at", "2026-09-24T07:40:00.123Z", `planned_at has no time zone in every_type (TIMESTAMP(6)), so an offset would be dropped: write "2026-09-24 07:40:00.123"`},
				{"zoned_at", "2026-09-24 07:40:00", `zoned_at holds a moment in every_type (TIMESTAMP(6) WITH TIME ZONE), which a time with no offset does not say: write its offset, like "2026-09-24T07:40:00Z"`},
				{"local_at", "2026-09-24", `local_at holds a moment in every_type (TIMESTAMP(6) WITH LOCAL TIME ZONE), which a time with no offset does not say`},
				{"planned_ms", "2026", `planned_ms is written as a string in every_type (TIMESTAMP(3)): write "2026"`},
				{"zoned_at", "1790244000", `zoned_at is written as a string in every_type (TIMESTAMP(6) WITH TIME ZONE): write "1790244000"`},
				{"local_at", "1790244000", `local_at is written as a string in every_type (TIMESTAMP(6) WITH LOCAL TIME ZONE): write "1790244000"`},
				{"span_ym", "1", `span_ym is written as a string in every_type (INTERVAL YEAR(2) TO MONTH): write "1"`},
				{"span_ds", "1", `span_ds is written as a string in every_type (INTERVAL DAY(2) TO SECOND(6)): write "1"`},
				{"token", "7", `token is written as a string in every_type (RAW): write "7"`},
				{"photo", "7", `photo is written as a string in every_type (BLOB): write "7"`},
				{"details", "3", `details is written as a string in every_type (JSON): write "3"`},
			},
			finds: [][2]string{
				{"street", "Müllerstraße"},
				{"recipient", "Jürgen"},
				{"depot", "H91"},
				{"zone_code", "DE1"},
				{"note", "left at the door"},
				{"n_note", "Jürgen signed"},
				{"grams", "-850"},
				{"tracking", "12345678901234567890123456789012345678"},
				{"hundreds", "1200"},
				{"pieces", "2"},
				{"tries", "3"},
				{"kilos", "0.5"},
				{"kilos", "0.50"},
				{"star_kilos", "-2.5"},
				{"fee", "1.5"},
				{"rate", "0.1"},
				{"ratio", "2.0"},
				{"huge", "1e20"},
				{"tiny", "1e-7"},
				{"b_float", "0.1"},
				{"b_double", "0.1"},
				{"b_whole", "2.0"},
				{"bare_whole", "7"},
				{"bare_decimal", "7.5"},
				{"bare_big", "12345678901234567890123"},
				{"bare_long", "0.1234567890123456789"},
				{"signed_for", "true"},
				{"returned", "false"},
				{"no_flag", "null"},
				{"day", "2026-09-24"},
				{"day_at", "2026-09-24 07:40:00"},
				{"day_at", "2026-09-24T07:40:00"},
				{"planned_at", "2026-09-24 07:40:00.123"},
				{"planned_ms", "2026-09-24 07:40:00.125"},
				{"zoned_at", "2026-09-24T07:40:00+02:00"},
				{"zoned_at", "2026-09-24T05:40:00Z"},
				{"local_at", "2026-09-24T07:40:00+02:00"},
				{"span_ym", "+01-02"},
				{"span_ds", "+01 02:03:04.000000"},
				{"token", "DEADBEEF"},
				{"photo", "DEADBEEF"},
				{"details", `{"zone":"DE-1","count":3}`},
				{"details", `{"count":3,"zone":"DE-1"}`},
				{"missing", "null"},
				{"bare_negative", "-0.25"},
				{"largest", "1e125"},
				{"smallest", "1e-125"},
			},
			misses: [][2]string{
				{"photo", "DEADBEEE"},
				{"details", `{"zone":"DE-1","count":"3"}`},
				{"details", `{"zone":"DE-1","count":3.5}`},
				{"bare_whole", "true"},
				{"bare_negative", "-0.26"},
				{"street", "Muellerstrasse"},
				{"depot", "H9"},
				{"note", "left at the gate"},
				{"grams", "850"},
				{"kilos", "0.51"},
				{"b_float", "0.2"},
				{"bare_whole", "7.0"},
				{"bare_decimal", "7"},
				{"bare_big", "12345678901234567890124"},
				{"bare_long", "0.1234567890123456788"},
				{"signed_for", "false"},
				{"returned", "true"},
				{"day_at", "2026-09-24"},
				{"planned_at", "2026-09-24 07:40:00"},
				{"zoned_at", "2026-09-24T07:40:00Z"},
			},
		}.run(t, h, true)
	})

	t.Run("every type's value in a column check", func(t *testing.T) {
		cellCases{
			table: "every_type",
			passes: [][2]string{
				{"street", "Müllerstraße"},
				{"recipient", "Jürgen"},
				{"depot", "H91"},
				{"zone_code", "DE1"},
				{"note", "left at the door"},
				{"n_note", "Jürgen signed"},
				{"grams", "-850"},
				{"tracking", "12345678901234567890123456789012345678"},
				{"hundreds", "1200"},
				{"pieces", "2"},
				{"tries", "3"},
				{"kilos", "0.5"},
				{"kilos", "0.50"},
				{"star_kilos", "-2.5"},
				{"fee", "1.5"},
				{"rate", "0.1"},
				{"ratio", "2.0"},
				{"huge", "1e20"},
				{"tiny", "1e-7"},
				{"b_float", "0.1"},
				{"b_double", "0.1"},
				{"b_whole", "2.0"},
				{"bare_whole", "7"},
				{"bare_decimal", "7.5"},
				{"bare_big", "12345678901234567890123"},
				{"bare_long", "0.1234567890123456789"},
				{"signed_for", "true"},
				{"returned", "false"},
				{"no_flag", "null"},
				{"day", "2026-09-24"},
				{"day_at", "2026-09-24 07:40:00"},
				{"day_at", "2026-09-24T07:40:00"},
				{"planned_at", "2026-09-24 07:40:00.123"},
				{"planned_ms", "2026-09-24 07:40:00.125"},
				{"zoned_at", "2026-09-24T07:40:00+02:00"},
				{"zoned_at", "2026-09-24T05:40:00Z"},
				{"local_at", "2026-09-24T07:40:00+02:00"},
				{"span_ym", "+01-02"},
				{"span_ds", "+01 02:03:04.000000"},
				{"token", "DEADBEEF"},
				{"photo", "DEADBEEF"},
				{"details", `{"zone":"DE-1","count":3}`},
				{"details", `{"count":3,"zone":"DE-1"}`},
				{"missing", "null"},
				{"GRAMS", "-850"},
				{"bare_negative", "-0.25"},
				{"largest", "1e125"},
				{"smallest", "1e-125"},
			},
			fails: [][3]string{
				{"street", "7", "differs"},
				{"depot", `"H91     "`, "differs"},
				{"note", "left at the gate", "differs"},
				{"missing", `""`, "differs"},
				{"grams", `"-850"`, `grams is the integer -850: write '-850'`},
				{"grams", "-850.0", "differs"},
				{"tracking", "12345678901234567890123456789012345679", "differs"},
				{"kilos", "1", "differs"},
				{"kilos", `"0.5"`, "0.5"},
				{"b_whole", "2", "differs"},
				{"bare_whole", "7.0", "differs"},
				{"bare_decimal", "7", "differs"},
				{"bare_big", "12345678901234567890124", "differs"},
				{"bare_long", "0.1234567890123456788", "differs"},
				{"signed_for", "false", "differs"},
				{"signed_for", "1", "differs"},
				{"signed_for", `"true"`, "differs"},
				{"returned", "0", "differs"},
				{"no_flag", "false", "differs"},
				{"day_at", "2026-09-24", "differs"},
				{"day_at", "2026-09-24T07:40:00+02:00", `day_at has no time zone (DATE), so an offset would be dropped: write '2026-09-24 07:40:00'`},
				{"planned_at", "2026-09-24T07:40:00.123Z", `write '2026-09-24 07:40:00.123'`},
				{"zoned_at", "2026-09-24 07:40:00", `zoned_at holds a moment (TIMESTAMP(6) WITH TIME ZONE), which a time with no offset does not say: write its offset, like '2026-09-24T07:40:00Z'`},
				{"local_at", "2026-09-24 07:40:00", `holds a moment`},
				{"day", "soon", "differs"},
				{"planned_at", "2026-09-24 07:40:00", "differs"},
				{"token", "DEADBEEE", "differs"},
				{"photo", "deadbeef", "differs"},
				{"zoned_at", "2026-09-24T07:40:00Z", "differs"},
				{"details", `{"zone":"DE-1","count":"3"}`, "differs"},
			},
		}.run(t, h)
	})

	t.Run("JSON in a JSON column and in a CLOB", func(t *testing.T) {
		ok(t, "a selection of rows is retrieved from the every_type table where:", []string{"reference", "PX-TYPES-1"})
		n := nth(len(mustService(t, h).Selections()))
		are := "the 1st row details property for the " + n + " selection json properties are:"
		ok(t, are, []string{"zone", "DE-1"}, []string{"count", "3"})
		if err := h.step(are, []string{"count", `"3"`}); err == nil {
			t.Error(`"3" matched the number 3 in a JSON column`)
		}
		if err := h.step(are, []string{"count", "3.0"}); err == nil {
			t.Error(`3.0 matched the integer 3 in a JSON column`)
		}
	})

	system, err := Resolve(fmt.Sprintf("oracle://%s:%s/FREEPDB1", host, port), "system", "Parcels-Secret-1")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "oracle", system.DSN,
		`CREATE PUBLIC SYNONYM every_type_public FOR parcels.every_type`,
		`CREATE TABLE system.handover (reference VARCHAR2(40) PRIMARY KEY, grams NUMBER(10), kilos NUMBER(10,2), signed_for BOOLEAN)`,
		`INSERT INTO system.handover VALUES ('PX-TYPES-1', 850, 2, TRUE)`,
		`GRANT SELECT ON system.handover TO parcels`,
		`CREATE PUBLIC SYNONYM handover_public FOR system.handover`,
	)

	t.Run("another schema's table, by its name and by a synonym", func(t *testing.T) {
		for _, table := range []string{"system.handover", "handover_public"} {
			sel := "a selection of rows is retrieved from the " + table + " table where:"
			for _, c := range [][3]string{
				{"reference", "7", `reference is text in ` + table},
				{"grams", "850.0", `grams is an integer in ` + table},
				{"kilos", "2", `kilos is a decimal in ` + table},
				{"signed_for", "1", `signed_for is a boolean in ` + table},
			} {
				if err := h.step(sel, []string{c[0], c[1]}); err == nil || !strings.Contains(err.Error(), c[2]) {
					t.Errorf("%s %s: %v, want %s", c[0], c[1], err, c[2])
				}
			}
			ok(t, sel, []string{"kilos", "2.0"}, []string{"signed_for", "true"})
			n := nth(len(mustService(t, h).Selections()))
			for column, value := range map[string]string{"grams": "850", "kilos": "2.00", "signed_for": "true"} {
				if err := h.step("the 1st row " + column + " column of the " + n + " selection is '" + value + "'"); err != nil {
					t.Errorf("%s of %s: %v", column, table, err)
				}
			}
		}
	})

	t.Run("a table named with its schema, a view and a synonym are held the same", func(t *testing.T) {
		for _, table := range []string{"parcels.every_type", "PARCELS.EVERY_TYPE", "every_type_view", "every_type_synonym", "every_type_again", "every_type_public"} {
			sel := "a selection of rows is retrieved from the " + table + " table where:"
			for _, c := range [][3]string{
				{"street", "7", `street is text in ` + table},
				{"grams", "-850.0", `grams is an integer in ` + table},
				{"kilos", "1", `kilos is a decimal in ` + table},
				{"signed_for", "1", `signed_for is a boolean in ` + table},
			} {
				if err := h.step(sel, []string{c[0], c[1]}); err == nil || !strings.Contains(err.Error(), c[2]) {
					t.Errorf("%s %s: %v, want %s", c[0], c[1], err, c[2])
				}
			}
			ok(t, sel, []string{"kilos", "0.5"}, []string{"signed_for", "true"})
			svc := mustService(t, h)
			all := svc.Selections()
			last := all[len(all)-1]
			if len(last.Rows) != 1 {
				t.Fatalf("%s: %d rows", table, len(last.Rows))
			}
			for column, want := range map[string]string{"kilos": "0.50", "grams": "-850", "signed_for": "true", "bare_decimal": "7.5"} {
				v, err := last.Value(0, column)
				if err != nil {
					t.Fatal(err)
				}
				if text(v) != want {
					t.Errorf("%s of %s reads as %v (%T), want %s", column, table, v, v, want)
				}
			}
		}
	})

	t.Run("a database it cannot reach is the step's failure", func(t *testing.T) {
		gone := newHarness(t)
		gone.ok("a parcels-db database with the following properties:", []string{"url", fmt.Sprintf("oracle://%s:%s/FREEPDB1", host, port)}, []string{"user", "parcels"}, []string{"password", "Parcels-Secret-1"})
		unreachable(t, gone, "every_type")
	})

	t.Run("a seed's relative time is the database's clock", func(t *testing.T) {
		db := openForTest(t, tgt)
		assertDatabaseZone(t, db, "SELECT EXTRACT(TIMEZONE_HOUR FROM SYSTIMESTAMP) * 60 + EXTRACT(TIMEZONE_MINUTE FROM SYSTIMESTAMP) FROM dual")
		relativeTimes(t, h, db, "clock_times", []string{"day_at", "plain_at", "zoned_at", "local_at"},
			`SELECT ROUND((SYSDATE - day_at) * 1440),
				ROUND((CAST(SYSTIMESTAMP AS DATE) - CAST(plain_at AS DATE)) * 1440),
				ROUND((CAST(SYS_EXTRACT_UTC(SYSTIMESTAMP) AS DATE) - CAST(SYS_EXTRACT_UTC(zoned_at) AS DATE)) * 1440),
				ROUND((CAST(SYS_EXTRACT_UTC(SYSTIMESTAMP) AS DATE) - CAST(SYS_EXTRACT_UTC(local_at) AS DATE)) * 1440)
				FROM clock_times WHERE reference = 'PX-CLOCK'`)
	})

	t.Run("a session's zone is the database's, not the machine's", func(t *testing.T) {
		sel, err := mustService(t, h).Query(t.Context(), "dual", "SELECT SESSIONTIMEZONE AS session_zone, DBTIMEZONE AS database_zone FROM dual")
		if err != nil {
			t.Fatal(err)
		}
		if len(sel.Rows) != 1 || text(sel.Rows[0][0]) != text(sel.Rows[0][1]) {
			t.Errorf("the session is in %v, the database in %v", sel.Rows[0][0], sel.Rows[0][1])
		}
	})

	t.Run("more at once than the pool has connections", func(t *testing.T) {
		oneConnectionAtATime(t, h, "load_parcels", "    kilos: 1.5\n    at_time: \"[MIN,MINUS,5]\"\n", true)
		manyAtOnce(t, h, "load_parcels", "")
	})

	t.Run("a seed sets every type", func(t *testing.T) {
		// Over Oracle's 4000 bytes for a literal, with a character of two bytes and a
		// quote at every place a cut could fall.
		long := strings.Repeat("Jürgen's note. ", 700)
		longJSON := `{"note":"` + strings.Repeat("left at the door. ", 300) + `"}`
		h.file("types.yaml", `seeded_types:
  - reference: PX-SEED-1
    street: Müllerstraße
    recipient: Jürgen
    depot: H91
    zone_code: DE1
    note: "`+long+`"
    n_note: "`+long+`"
    grams: -850
    tracking: 12345678901234567890123
    kilos: 0.5
    fee: 1.5
    b_float: 0.5
    b_double: 0.1
    bare_whole: 7
    bare_decimal: 7.5
    signed_for: true
    returned: false
    day: "2026-09-24"
    planned_ms: "2026-09-24 07:40:00.125"
    zoned_at: "2026-09-24T07:40:00+02:00"
    local_at: "2026-09-24T07:40:00+02:00"
    span_ym: "+01-02"
    span_ds: "+01 02:03:04.000000"
    token: DEADBEEF
    photo: DEADBEEF
    details:
      zone: DE-1
      count: 3
  - reference: PX-SEED-2
    details: '`+longJSON+`'
`)
		ok(t, "a types.yaml db seed")
		ok(t, "a selection of rows is retrieved from the seeded_types table where:", []string{"reference", "PX-SEED-1"}, []string{"signed_for", "true"}, []string{"returned", "false"})
		n := nth(len(mustService(t, h).Selections()))
		for column, value := range map[string]string{
			"street": "Müllerstraße", "recipient": "Jürgen", "depot": "H91", "zone_code": "DE1",
			"grams": "-850", "kilos": "0.5", "fee": "1.5", "b_float": "0.5", "b_double": "0.1", "bare_whole": "7", "bare_decimal": "7.5",
			"signed_for": "true", "returned": "false", "day": "2026-09-24", "planned_ms": "2026-09-24 07:40:00.125",
			"zoned_at": "2026-09-24T05:40:00Z", "local_at": "2026-09-24T05:40:00Z", "span_ym": "+01-02", "span_ds": "+01 02:03:04.000000", "token": "DEADBEEF", "photo": "DEADBEEF",
			"details": `{"zone":"DE-1","count":3}`,
		} {
			if err := h.step("the 1st row " + column + " column of the " + n + " selection is '" + value + "'"); err != nil {
				t.Errorf("%s: %v", column, err)
			}
		}
		for _, column := range []string{"note", "n_note"} {
			if err := h.step("the 1st row " + column + ` column of the ` + n + ` selection is "` + long + `"`); err != nil {
				t.Errorf("a long %s: %v", column, err)
			}
			ok(t, "a selection of rows is retrieved from the seeded_types table where:", []string{column, `"` + long + `"`})
			all := mustService(t, h).Selections()
			if got := len(all[len(all)-1].Rows); got != 1 {
				t.Errorf("a long %s selected %d rows", column, got)
			}
		}
		ok(t, "a selection of rows is retrieved from the seeded_types table where:", []string{"reference", "PX-SEED-2"}, []string{"details", longJSON})
		all := mustService(t, h).Selections()
		if got := len(all[len(all)-1].Rows); got != 1 {
			t.Errorf("long JSON selected %d rows", got)
		}
	})

	t.Run("a trigger's condition is held to its column", func(t *testing.T) {
		for _, c := range [][3]string{
			{"signed_for", "1", "is a boolean"},
			{"grams", "-850.0", "is an integer"},
			{"kilos", "1", "is a decimal"},
			{"street", "7", "is text"},
			{"note", "7", "is text"},
			{"day", "2026", "written as a string"},
			{"details", "3", "written as a string"},
			{"note", `""`, "write null"},
		} {
			err := h.step("a before insert trigger on the every_type table will raise an ORA-20001 exception where:", []string{c[0], c[1]})
			if err == nil || !strings.Contains(err.Error(), c[2]) {
				t.Errorf("a trigger on %s %s: %v, want %s", c[0], c[1], err, c[2])
			}
		}
		if got := len(mustService(t, h).Triggers()); got != 0 {
			t.Errorf("%d triggers were created", got)
		}
	})

	t.Run("before update and before delete triggers", func(t *testing.T) {
		db := openForTest(t, tgt)
		for _, row := range triggerRows("trigger_parcels") {
			if _, err := db.Exec(row); err != nil {
				t.Fatal(err)
			}
		}
		updateAndDeleteTriggers(t, h, db, "trigger_parcels", "ORA-20001")
		// A number outside RAISE_APPLICATION_ERROR's range, as a deadlock's.
		if _, err := db.Exec("INSERT INTO trigger_parcels (reference, status, kilos) VALUES ('PX-TRG-D', 'REGISTERED', 1.5)"); err != nil {
			t.Fatal(err)
		}
		ok(t, "a before update trigger on the trigger_parcels table will raise a 00060 exception where:", []string{"reference", "PX-TRG-D"})
		if _, err := db.Exec("UPDATE trigger_parcels SET status = 'PICKED_UP' WHERE reference = 'PX-TRG-D'"); err == nil || !strings.Contains(err.Error(), "ORA-00060") {
			t.Errorf("a deadlock's number: %v", err)
		}
		if err := mustService(t, h).dropTriggers(t.Context()); err != nil {
			t.Fatal(err)
		}
		// What the catalog's types ask of a condition holds of the row a delete takes
		// away, and of the row an update leaves.
		for _, c := range []struct{ column, cell, literal string }{
			{"note", "left at the door", "'left at the door'"},
			{"signed_for", "true", "TRUE"},
			{"details", `{"zone":"DE-1"}`, `JSON('{"zone":"DE-1"}')`},
			{"day", "2026-09-24", "DATE '2026-09-24'"},
		} {
			store := func(reference string) {
				t.Helper()
				if _, err := db.Exec("INSERT INTO every_type (reference, " + c.column + ") VALUES ('" + reference + "', " + c.literal + ")"); err != nil {
					t.Fatal(err)
				}
			}
			ok(t, "a before delete trigger on the every_type table will raise an ORA-20001 exception where:", []string{c.column, c.cell})
			store("PX-TRG-DEL")
			if _, err := db.Exec("DELETE FROM every_type WHERE reference = 'PX-TRG-DEL'"); err == nil || !strings.Contains(err.Error(), "ORA-20001") {
				t.Errorf("a delete of a row whose %s is %s: %v", c.column, c.cell, err)
			}
			if err := mustService(t, h).dropTriggers(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("DELETE FROM every_type WHERE reference = 'PX-TRG-DEL'"); err != nil {
				t.Fatal(err)
			}
			store("PX-TRG-UPD")
			ok(t, "a before update trigger on the every_type table will raise an ORA-20001 exception where:", []string{c.column, c.cell})
			if _, err := db.Exec("UPDATE every_type SET grams = 1 WHERE reference = 'PX-TRG-UPD'"); err == nil || !strings.Contains(err.Error(), "ORA-20001") {
				t.Errorf("an update of a row whose %s is %s: %v", c.column, c.cell, err)
			}
			if err := mustService(t, h).dropTriggers(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("DELETE FROM every_type WHERE reference = 'PX-TRG-UPD'"); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("a trigger fires on what its condition says, of every kind of column", func(t *testing.T) {
		db := openForTest(t, tgt)
		for i, c := range []struct{ column, cell, match, other string }{
			{"signed_for", "true", "TRUE", "FALSE"},
			{"returned", "false", "FALSE", "TRUE"},
			{"grams", "850", "850", "851"},
			{"kilos", "1.5", "1.5", "1.6"},
			{"bare_decimal", "7.5", "7.5", "7.6"},
			{"b_double", "1.5", "1.5", "1.6"},
			{"street", "Müllerstraße", "'Müllerstraße'", "'Müllerstrasse'"},
			{"recipient", "Jürgen", "'Jürgen'", "'Juergen'"},
			{"depot", "H91", "'H91'", "'H92'"},
			{"note", "left at the door", "'left at the door'", "'left at the gate'"},
			{"n_note", "Jürgen signed", "'Jürgen signed'", "'Jürgen did not sign'"},
			{"token", "DEADBEEF", "HEXTORAW('DEADBEEF')", "HEXTORAW('DEADBEEE')"},
			{"photo", "DEADBEEF", "HEXTORAW('DEADBEEF')", "HEXTORAW('DEADBEEE')"},
			{"details", `{"zone":"DE-1","count":3}`, `JSON('{"count":3,"zone":"DE-1"}')`, `JSON('{"zone":"DE-1","count":"3"}')`},
			{"day", "2026-09-24", "DATE '2026-09-24'", "DATE '2026-09-25'"},
			{"planned_at", "2026-09-24 07:40:00", "TIMESTAMP '2026-09-24 07:40:00'", "TIMESTAMP '2026-09-25 07:40:00'"},
			{"zoned_at", "2026-09-24T07:40:00+02:00", "TIMESTAMP '2026-09-24 05:40:00 +00:00'", "TIMESTAMP '2026-09-24 07:40:00 +00:00'"},
		} {
			if err := h.step("a before insert trigger on the every_type table will raise an ORA-20001 exception where:", []string{c.column, c.cell}); err != nil {
				t.Errorf("a trigger on %s %s: %v", c.column, c.cell, err)
				continue
			}
			insert := func(suffix, value string) error {
				_, err := db.Exec(fmt.Sprintf("INSERT INTO every_type (reference, %s) VALUES ('PX-FIRE-%d-%s', %s)", c.column, i, suffix, value))
				return err
			}
			if err := insert("MATCH", c.match); err == nil || !strings.Contains(err.Error(), "ORA-20001") {
				t.Errorf("%s: inserting %s: %v, want ORA-20001", c.column, c.match, err)
			}
			if err := insert("OTHER", c.other); err != nil {
				t.Errorf("%s: inserting %s, which it does not match: %v", c.column, c.other, err)
			}
			if err := mustService(t, h).dropTriggers(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec("DELETE FROM every_type WHERE reference LIKE 'PX-FIRE-%'"); err != nil {
			t.Fatal(err)
		}
	})
}
