// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package render_test

import (
	"io/ioutil"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/render"
)

func Test(t *testing.T) { TestingT(t) }

type renderSuite struct {
	stdout *os.File
}

func (s *renderSuite) StdoutText() string {
	s.stdout.Seek(0, 0)
	blob, err := ioutil.ReadAll(s.stdout)
	if err != nil {
		panic(err)
	}
	return string(blob)
}

var _ = Suite(&renderSuite{})

func (s *renderSuite) SetUpTest(c *C) {
	f, err := ioutil.TempFile("", "stdout-*.txt")
	c.Assert(err, IsNil)
	s.stdout = f
}

func (s *renderSuite) TearDownTest(c *C) {
	if s.stdout != nil {
		defer s.stdout.Close()
		os.Remove(s.stdout.Name())
	}
}

func (s *renderSuite) TestComposeStripes(c *C) {
	render.ComposeStripes(s.stdout, []render.Stripe{
		{X: 10, Y: 2, ScanLine: "Render   "},
		{X: 10, Y: 3, ScanLine: "This  "},
		{X: 8, Y: 2, ScanLine: "-"},
		{X: 8, Y: 2, ScanLine: "*"}, // overlap
	})
	c.Check(s.StdoutText(), Equals, ""+
		"\n"+
		"\n"+
		"        * Render\n"+
		"          This\n"+
		"")
}

func (s *renderSuite) TestComposeHiragana(c *C) {
	render.ComposeStripes(s.stdout, []render.Stripe{{ScanLine: "ひ"}})
	c.Check(s.StdoutText(), Equals, "ひ\n")
}

func (s *renderSuite) TestComposeOverwriteDoubleWidth(c *C) {
	render.ComposeStripes(s.stdout, []render.Stripe{
		{X: 0, Y: 0, ScanLine: "0123456789"},
		{X: 0, Y: 1, ScanLine: "abcdefghij"},
		// Overwrite 'b' and 'c' above with hiragana 'HI' syllable.
		{X: 1, Y: 1, ScanLine: "ひ"},
	})
	c.Check(s.StdoutText(), Equals, ""+
		"0123456789\n"+
		"aひdefghij\n"+
		"")
}
