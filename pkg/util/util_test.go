//
// Use and distribution licensed under the Apache license version 2.
//
// See the COPYING file in the root project directory for full text.
//

package util_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zededa/ghw/pkg/util"
)

// nolint: gocyclo
func TestConcatStrings(t *testing.T) {
	type testCase struct {
		items    []string
		expected string
	}

	testCases := []testCase{
		{
			items:    []string{},
			expected: "",
		},
		{
			items:    []string{"simple"},
			expected: "simple",
		},
		{
			items: []string{
				"foo",
				"bar",
				"baz",
			},
			expected: "foobarbaz",
		},
		{
			items: []string{
				"foo ",
				" bar ",
				" baz",
			},
			expected: "foo  bar  baz",
		},
	}

	for _, tCase := range testCases {
		t.Run(tCase.expected, func(t *testing.T) {
			got := util.ConcatStrings(tCase.items...)
			if got != tCase.expected {
				t.Errorf("expected %q got %q", tCase.expected, got)
			}
		})
	}
}

func TestParseBool(t *testing.T) {
	type testCase struct {
		item     string
		expected bool
	}

	testCases := []testCase{
		{
			item:     "False",
			expected: false,
		},
		{
			item:     "F",
			expected: false,
		},
		{
			item:     "1",
			expected: true,
		},
		{
			item:     "",
			expected: false,
		},
		{
			item:     "on",
			expected: true,
		},
		{
			item:     "Off",
			expected: false,
		},
		{
			item:     "Yes",
			expected: true,
		},
		{
			item:     "no",
			expected: false,
		},
	}

	for _, tCase := range testCases {
		t.Run(tCase.item, func(t *testing.T) {
			got, err := util.ParseBool(tCase.item)
			if got != tCase.expected {
				t.Errorf("expected %t got %t", tCase.expected, got)
			}
			if err != nil {
				t.Errorf("util.ParseBool threw error %s", err)
			}
		})
	}
}

func TestParseCPUList(t *testing.T) {
	testCases := []struct {
		in      string
		want    []int
		wantErr bool
	}{
		{in: "0-3,7\n", want: []int{0, 1, 2, 3, 7}},
		{in: "5", want: []int{5}},
		{in: "0,2-3", want: []int{0, 2, 3}},
		{in: "", want: nil},
		{in: " \n", want: nil},
		{in: "(null)\n", want: nil},
		{in: "a", wantErr: true},
		{in: "3-1", wantErr: true},
		{in: "1-", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "0,,1", wantErr: true},
		{in: "65535", want: []int{65535}},
		{in: "65536", wantErr: true},
		{in: "0-2147483647", wantErr: true},
	}
	for _, tc := range testCases {
		got, err := util.ParseCPUList(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseCPUList(%q) error = %v, want error %v", tc.in, err, tc.wantErr)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseCPUList(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestReadCPUListAndInt(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if got, err := util.ReadCPUList(write("list", "0-2,8\n")); err != nil || !reflect.DeepEqual(got, []int{0, 1, 2, 8}) {
		t.Errorf("ReadCPUList = %v, %v; want [0 1 2 8]", got, err)
	}
	if _, err := util.ReadCPUList(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadCPUList(missing) error = %v, want os.ErrNotExist", err)
	}
	if _, err := util.ReadCPUList(write("bad", "x\n")); err == nil {
		t.Error("ReadCPUList(malformed) = no error")
	}
	if got, err := util.ReadInt(write("int", " -1\n")); err != nil || got != -1 {
		t.Errorf("ReadInt = %d, %v; want -1", got, err)
	}
	if _, err := util.ReadInt(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadInt(missing) error = %v, want os.ErrNotExist", err)
	}
	if _, err := util.ReadInt(write("nan", "abc")); err == nil {
		t.Error("ReadInt(abc) = no error")
	}
}
