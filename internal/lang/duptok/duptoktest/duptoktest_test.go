package duptoktest

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// sink is the part of duptok.Stream and Recorder a tokenizer writes to.
type sink interface {
	Add(code int32, class duptok.Class, line, last int) error
	EndFile(name string, code []bool)
}

// tee writes every token and file to both sinks.
type tee struct{ a, b sink }

func (t tee) Add(code int32, class duptok.Class, line, last int) error {
	if err := t.a.Add(code, class, line, last); err != nil {
		return err
	}
	return t.b.Add(code, class, line, last)
}

func (t tee) EndFile(name string, code []bool) {
	t.a.EndFile(name, code)
	t.b.EndFile(name, code)
}

// write fills s with files built from fixed and random pieces: a repeated
// code body, literal tables with and without signs, and noise, two tokens
// to a line with every third line not a source line.
func write(t *testing.T, s sink, files int) {
	t.Helper()
	rng := rand.New(rand.NewPCG(1, 2))
	body := make([]int32, 60)
	for i := range body {
		body[i] = int32(1 + rng.IntN(20))
	}
	for f := range files {
		var toks []int32
		var classes []duptok.Class
		add := func(code int32, class duptok.Class) {
			toks = append(toks, code)
			classes = append(classes, class)
		}
		for range 3 {
			for range 5 + rng.IntN(30) {
				add(int32(1+rng.IntN(20)), duptok.Code)
			}
			switch rng.IntN(3) {
			case 0:
				for _, c := range body {
					add(c, duptok.Code)
				}
			case 1:
				for range 30 {
					add(100, duptok.Literal)
					add(101, duptok.Punct)
				}
			default:
				for range 20 {
					add(102, duptok.Sign)
					add(100, duptok.Literal)
					add(101, duptok.Punct)
				}
			}
		}
		lines := len(toks)/2 + 2
		code := make([]bool, lines+1)
		for ln := range code {
			code[ln] = ln%3 != 0
		}
		for i, c := range toks {
			if err := s.Add(c, classes[i], i/2+1, i/2+1+i%5/4); err != nil {
				t.Fatal(err)
			}
		}
		s.EndFile("f"+strconv.Itoa(f)+".go", code)
	}
}

func TestRecorderMatchesStream(t *testing.T) {
	opts := []duptok.Options{
		{MinTokens: 40},
		{MinTokens: 40, IgnoreLiteralOnly: true},
		duptok.DefaultOptions(),
		{MinTokens: 10, IgnoreLiteralOnly: true, FoldSigns: true},
	}
	for _, o := range opts {
		name := strconv.Itoa(o.MinTokens) + "/" + strconv.FormatBool(o.IgnoreLiteralOnly) + "/" + strconv.FormatBool(o.FoldSigns)
		t.Run(name, func(t *testing.T) {
			var s duptok.Stream
			var r Recorder
			write(t, tee{&s, &r}, 6)
			const sloc = 400
			want, err := s.Count(o, sloc)
			if err != nil {
				t.Fatal(err)
			}
			blocks, err := r.Blocks(o)
			if err != nil {
				t.Fatal(err)
			}
			if want.Blocks == 0 {
				t.Fatal("stream has no blocks; the test input is too weak")
			}
			got, per := r.Count(blocks, sloc)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Count = %+v\nwant %+v", got, want)
			}
			total := 0
			for _, n := range per {
				total += n
			}
			if pct := duptok.Percent(total, sloc); pct != want.Pct {
				t.Errorf("per-file covered lines sum to %d (%v%%), want %v%%", total, pct, want.Pct)
			}
			for _, b := range blocks {
				first := r.Tokens[b.Pos[0] : b.Pos[0]+b.N]
				for _, p := range b.Pos[1:] {
					if !slices.EqualFunc(first, r.Tokens[p:p+b.N], func(x, y Token) bool { return x.Code == y.Code }) {
						t.Errorf("block at %v, %d tokens: occurrence %d differs from the first", b.Pos, b.N, p)
					}
				}
			}
		})
	}
}

func TestRecorderRejectsBadCode(t *testing.T) {
	var r Recorder
	for _, c := range []int32{-1, duptok.SeparatorBase} {
		if err := r.Add(c, duptok.Code, 1, 1); err == nil {
			t.Errorf("Add(%d): no error", c)
		}
	}
	if _, err := r.Blocks(duptok.Options{}); err == nil {
		t.Error("Blocks with MinTokens 0: no error")
	}
}
