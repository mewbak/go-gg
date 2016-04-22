// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package table implements ordered, grouped two dimensional relations.
//
// There are two related abstractions: Table and Grouping.
//
// A Table is an ordered relation of rows and columns. Each column is
// a Go slice and hence must be homogeneously typed, but different
// columns may have different types. All columns in a Table have the
// same number of rows.
//
// A Grouping generalizes a Table by grouping the Table's rows into
// zero or more groups. A Table is itself a Grouping with zero or one
// groups. Most operations take a Grouping and operate on each group
// independently, though some operations sub-divide or combine groups.
//
// The structures of both Tables and Groupings are immutable. Adding a
// column to a Table returns a new Table and adding a new Table to a
// Grouping returns a new Grouping.
package table

import (
	"fmt"
	"reflect"

	"github.com/aclements/go-gg/generic"
)

// TODO
//
// Rename Table to T?
//
// Make Add replace column in place?
//
// Require same columns sequence for adding a table?
//
// Make Table an interface? Then columns could be constructed lazily.
//
// Have separate builder/viewer APIs?

// A Table is an ordered two dimensional relation. It consists of a
// set of named columns. Each column is a sequence of values of a
// consistent type or a constant value. All (non-constant) columns
// have the same length.
//
// The zero value of Table is the "empty table": it has no rows and no
// columns. Note that a Table may have one or more columns, but no
// rows; such a Table is *not* considered empty.
//
// A Table is also a trivial Grouping. If a Table is empty, it has no
// groups and hence the zero value of Table is also the "empty group".
// Otherwise, it consists only of the root group, RootGroupID.
//
// A Table's structure is immutable. To construct a Table, start with
// an empty table and add columns to it using Add.
type Table struct {
	cols     map[string]Slice
	consts   map[string]interface{}
	colNames []string
	len      int
}

// A Grouping is a set of tables with identical sets of columns, each
// identified by a distinct GroupID.
//
// Visually, a Grouping can be thought of as follows:
//
//	   Col A  Col B  Col C
//	------ group /a ------
//	0   5.4    "x"     90
//	1   -.2    "y"     30
//	------ group /b ------
//	0   9.3    "a"     10
//
// Like a Table, a Grouping's structure is immutable. To construct a
// Grouping, start with a Table (typically the empty Table, which is
// also the empty Grouping) and add tables to it using AddTable.
//
// Despite the fact that GroupIDs form a hierarchy, a Grouping ignores
// this hierarchy and simply operates on a flat map of distinct
// GroupIDs to Tables.
type Grouping interface {
	// Columns returns the names of the columns in this Grouping,
	// or nil if there are no Tables or the group consists solely
	// of empty Tables. All Tables in this Grouping have the same
	// set of columns.
	Columns() []string

	// Tables returns the group IDs of the tables in this
	// Grouping.
	Tables() []GroupID

	// Table returns the Table in group gid, or nil if there is no
	// such Table.
	Table(gid GroupID) *Table

	// AddTable returns a new Grouping with the addition of Table
	// t bound to group gid. If t is nil, it returns a Grouping
	// with group gid removed. If t is the empty Table, this is a
	// no-op because the empty Table contains no groups.
	// Otherwise, AddTable first removes group gid if it already
	// exists, and then adds t as a new group. Table t must have
	// the same set of columns as any existing Tables in this
	// group and they must have identical types; otherwise,
	// AddTable will panic.
	//
	// TODO The same set or the same sequence of columns? Given
	// that I never use the sequence (except maybe for printing),
	// perhaps it doesn't matter.
	//
	// TODO This doesn't make it easy to combine two Groupings. It
	// could instead take a Grouping and reparent it.
	AddTable(gid GroupID, t *Table) Grouping
}

type groupedTable struct {
	tables   map[GroupID]*Table
	groups   []GroupID
	colNames []string
}

// A Slice is a Go slice value.
//
// This is primarily for documentation. There is no way to statically
// enforce this in Go; however, functions that expect a Slice will
// panic with a *generic.TypeError if passed a non-slice value.
type Slice interface{}

func reflectSlice(s Slice) reflect.Value {
	rv := reflect.ValueOf(s)
	if rv.Kind() != reflect.Slice {
		panic(&generic.TypeError{rv.Type(), nil, "is not a slice"})
	}
	return rv
}

// Add returns a new Table with a new column bound to data, or removes
// the named column if data is nil. If Table t already has a column
// with the given name, Add first removes it. Then, if data is
// non-nil, Add adds a new column. If data is non-nil, it must have
// the same length as any existing columns or Add will panic.
//
// TODO: "Add" suggests mutation. Should this be called "Plus"?
func (t *Table) Add(name string, data Slice) *Table {
	// TODO: Currently adding N columns is O(N^2). If we built the
	// column index only when it was asked for, the usual case of
	// adding a bunch of columns and then using the final table
	// would be O(N).

	if data == nil {
		// Remove the column.
		if _, ok := t.cols[name]; !ok {
			if _, ok := t.consts[name]; !ok {
				// Nothing to remove.
				return t
			}
		}
		if len(t.colNames) == 1 {
			// We're removing the only column. This is now
			// an empty table.
			return &Table{}
		}
	}

	// Create the new table, removing any existing column with the
	// same name.
	nt := t.cloneSans(name)
	if data == nil {
		return nt
	}

	rv := reflectSlice(data)
	dataLen := rv.Len()
	if len(nt.cols) == 0 {
		// First non-constant column.
		nt.cols[name] = data
		nt.len = dataLen
	} else if nt.len != dataLen {
		panic(fmt.Sprintf("cannot add column %q with %d elements to table with %d rows", name, dataLen, nt.len))
	} else {
		nt.cols[name] = data
	}
	nt.colNames = append(nt.colNames, name)

	return nt
}

// AddConst returns a new Table with a new constant column whose value
// is val. If Table t already has a column with this name, AddConst
// first removes it.
//
// A constant column has the same value in every row of the Table. It
// does not itself have an inherent length.
func (t *Table) AddConst(name string, val interface{}) *Table {
	// Clone and remove any existing column with the same name.
	nt := t.cloneSans(name)
	nt.consts[name] = val
	nt.colNames = append(nt.colNames, name)
	return nt
}

// cloneSans returns a clone of t without column name.
func (t *Table) cloneSans(name string) *Table {
	// Create the new table, removing any existing column with the
	// same name.
	nt := &Table{make(map[string]Slice), make(map[string]interface{}), []string{}, t.len}
	for _, name2 := range t.colNames {
		if name != name2 {
			if c, ok := t.cols[name2]; ok {
				nt.cols[name2] = c
			}
			if cv, ok := t.consts[name2]; ok {
				nt.consts[name2] = cv
			}
			nt.colNames = append(nt.colNames, name2)
		}
	}
	return nt
}

// Len returns the number of rows in Table t.
func (t *Table) Len() int {
	return t.len
}

// Columns returns the names of the columns in Table t, or nil if this
// Table is empty.
func (t *Table) Columns() []string {
	return t.colNames
}

// Column returns the slice of data in column name of Table t, or nil
// if there is no such column. If name is a constant column, this
// returns a slice with the constant value repeated to the length of
// the Table.
func (t *Table) Column(name string) Slice {
	if c, ok := t.cols[name]; ok {
		// It's a regular column or a constant column with a
		// cached expansion.
		return c
	}

	if cv, ok := t.consts[name]; ok {
		// Expand the constant column and cache the result.
		expanded := generic.Repeat(cv, t.len)
		t.cols[name] = expanded
		return expanded
	}

	return nil
}

// MustColumn is like Column, but panics if there is no such column.
func (t *Table) MustColumn(name string) Slice {
	if c := t.Column(name); c != nil {
		return c
	}
	panic(fmt.Sprintf("unknown column %q", name))
}

// Const returns the value of constant column name. If this column
// does not exist or is not a constant column, Const returns nil,
// false.
func (t *Table) Const(name string) (val interface{}, ok bool) {
	cv, ok := t.consts[name]
	return cv, ok
}

// isEmpty returns true if t is an empty Table, meaning it has no rows
// or columns.
func (t *Table) isEmpty() bool {
	return t.colNames == nil
}

// Tables returns the groups IDs in this Table. If t is empty, there
// are no group IDs. Otherwise, there is only RootGroupID.
func (t *Table) Tables() []GroupID {
	if t.isEmpty() {
		return []GroupID{}
	}
	return []GroupID{RootGroupID}
}

// Table returns t if gid is RootGroupID and t is not empty; otherwise
// it returns nil.
func (t *Table) Table(gid GroupID) *Table {
	if gid == RootGroupID && !t.isEmpty() {
		return t
	}
	return nil
}

// AddTable returns a Grouping with up to two groups: first, t, if
// non-empty, is bound to RootGroupID; then t2, if non-empty, is bound
// to group gid.
//
// Typically this is used to build up a Grouping by starting with an
// empty Table and adding Tables to it.
func (t *Table) AddTable(gid GroupID, t2 *Table) Grouping {
	if t2 == nil {
		if gid == RootGroupID {
			return new(Table)
		}
		return t
	} else if t2.isEmpty() {
		return t
	} else if gid == RootGroupID {
		return t2
	}

	g := &groupedTable{
		tables:   map[GroupID]*Table{},
		groups:   []GroupID{},
		colNames: nil,
	}
	return g.AddTable(RootGroupID, t).AddTable(gid, t2)
}

func (g *groupedTable) Columns() []string {
	return g.colNames
}

func (g *groupedTable) Tables() []GroupID {
	return g.groups
}

func (g *groupedTable) Table(gid GroupID) *Table {
	return g.tables[gid]
}

func (g *groupedTable) AddTable(gid GroupID, t *Table) Grouping {
	// TODO: Currently adding N tables is O(N^2).

	if t != nil && t.isEmpty() {
		// Adding an empty table has no effect.
		return g
	}

	// Create the new grouped table, removing any existing table
	// with the same GID.
	ng := &groupedTable{map[GroupID]*Table{}, []GroupID{}, g.colNames}
	for _, gid2 := range g.groups {
		if gid != gid2 {
			ng.tables[gid2] = g.tables[gid2]
			ng.groups = append(ng.groups, gid2)
		}
	}
	if t == nil {
		if len(ng.groups) == 0 {
			ng.colNames = nil
		}
		return ng
	}

	if len(ng.groups) == 0 {
		ng.tables[gid] = t
		ng.groups = append(ng.groups, gid)
		ng.colNames = t.Columns()
		return ng
	}

	// Check that t's column structure matches.
	tBase := ng.tables[ng.groups[0]]
	for _, col := range ng.colNames {
		var t0, t1 reflect.Type
		if c, ok := tBase.cols[col]; ok {
			t0 = reflect.TypeOf(c).Elem()
		} else {
			t0 = reflect.TypeOf(tBase.consts[col])
		}
		if c, ok := t.cols[col]; ok {
			t1 = reflect.TypeOf(c).Elem()
		} else if cv, ok := t.consts[col]; ok {
			t1 = reflect.TypeOf(cv)
		} else {
			panic(fmt.Sprintf("table missing column %q", col))
		}
		if t0 != t1 {
			panic(&generic.TypeError{t0, t1, fmt.Sprintf("for column %q are not the same", col)})
		}
	}
	if len(t.cols) != len(ng.colNames) {
		// t has a column the group doesn't.
		colSet := map[string]bool{}
		for _, col := range ng.colNames {
			colSet[col] = true
		}
		for col := range t.cols {
			if !colSet[col] {
				panic(fmt.Sprintf("table has extra column %q", col))
			}
		}
	}

	ng.tables[gid] = t
	ng.groups = append(ng.groups, gid)
	return ng
}

// addTableUpdate adds table t to g by mutation. It assumes that the
// column structure matches. This is meant for internal use only.
//
// TODO: It would be nice if external users could achieve similar
// asymptotic performance safely. There could be a GroupBuilder API
// that does this and then freezes into a Grouped.
func (g *groupedTable) addTableUpdate(gid GroupID, t *Table) {
	if len(g.groups) == 0 {
		g.tables = map[GroupID]*Table{gid: t}
		g.groups = []GroupID{gid}
		g.colNames = t.Columns()
		return
	}

	if _, ok := g.tables[gid]; !ok {
		g.groups = append(g.groups, gid)
	}
	g.tables[gid] = t
}
