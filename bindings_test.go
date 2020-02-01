package sitter

import (
	"bytes"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootNode(t *testing.T) {
	assert := assert.New(t)

	n := Parse([]byte("1 + 2"), getTestGrammar())

	assert.Equal(uint32(0), n.StartByte())
	assert.Equal(uint32(5), n.EndByte())
	assert.Equal(Point{
		Row:    0,
		Column: 0,
	}, n.StartPoint())
	assert.Equal(Point{
		Row:    0,
		Column: 5,
	}, n.EndPoint())
	assert.Equal("(expression (sum left: (expression (number)) right: (expression (number))))", n.String())
	assert.Equal("expression", n.Type())
	assert.Equal(Symbol(7), n.Symbol())

	assert.Equal(false, n.IsNull())
	assert.Equal(true, n.IsNamed())
	assert.Equal(false, n.IsMissing())
	assert.Equal(false, n.HasChanges())
	assert.Equal(false, n.HasError())

	assert.Equal(uint32(1), n.ChildCount())
	assert.Equal(uint32(1), n.NamedChildCount())

	assert.Nil(n.Parent())
	assert.Nil(n.NextSibling())
	assert.Nil(n.NextNamedSibling())
	assert.Nil(n.PrevSibling())
	assert.Nil(n.PrevNamedSibling())

	assert.NotNil(n.Child(0))
	assert.NotNil(n.NamedChild(0))
	assert.Nil(n.ChildByFieldName("unknown"))

	assert.NotNil(n.NamedChild(0).ChildByFieldName("left"))
}

func TestTree(t *testing.T) {
	assert := assert.New(t)

	parser := NewParser()

	parser.Debug()
	parser.SetLanguage(getTestGrammar())
	tree := parser.ParseString(nil, []byte("1 + 2"))
	n := tree.RootNode()

	assert.Equal(uint32(0), n.StartByte())
	assert.Equal(uint32(5), n.EndByte())
	assert.Equal("expression", n.Type())
	assert.Equal("(expression (sum left: (expression (number)) right: (expression (number))))", n.String())

	// change 2 -> (3 + 3)
	newText := []byte("1 + (3 + 3)")
	tree.Edit(EditInput{
		StartIndex:  4,
		OldEndIndex: 5,
		NewEndIndex: 11,
		StartPoint: Point{
			Row:    0,
			Column: 4,
		},
		OldEndPoint: Point{
			Row:    0,
			Column: 5,
		},
		NewEndPoint: Point{
			Row:    0,
			Column: 11,
		},
	})
	// check that it changed tree
	assert.True(n.HasChanges())
	assert.True(n.Child(0).HasChanges())
	assert.False(n.Child(0).Child(0).HasChanges()) // left side of the sum didn't change
	assert.True(n.Child(0).Child(2).HasChanges())

	tree2 := parser.ParseString(tree, newText)
	n = tree2.RootNode()
	assert.Equal("(expression (sum left: (expression (number)) right: (expression (expression (sum left: (expression (number)) right: (expression (number)))))))", n.String())
}

func TestLanguage(t *testing.T) {
	assert := assert.New(t)
	js := getTestGrammar()

	assert.Equal(uint32(9), js.SymbolCount())
	assert.Equal(js.SymbolName(3), "+")
	assert.Equal(js.SymbolType(3), SymbolTypeAnonymous)
	assert.Equal(js.SymbolName(4), "number")
	assert.Equal(js.SymbolType(4), SymbolTypeRegular)

	assert.Equal(SymbolTypeRegular.String(), "Regular")
}

func TestGC(t *testing.T) {
	assert := assert.New(t)

	parser := NewParser()

	parser.SetLanguage(getTestGrammar())
	tree := parser.ParseString(nil, []byte("1 + 2"))
	n := tree.RootNode()

	r := isNamedWithGC(n)
	assert.True(r)
}

func isNamedWithGC(n *Node) bool {
	runtime.GC()
	time.Sleep(500 * time.Microsecond)
	return n.IsNamed()
}

func TestSetOperationLimit(t *testing.T) {
	assert := assert.New(t)

	parser := NewParser()
	assert.Equal(0, parser.OperationLimit())

	parser.SetOperationLimit(10)
	assert.Equal(10, parser.OperationLimit())
}

func TestIncludedRanges(t *testing.T) {
	assert := assert.New(t)

	// sum code with sum code in a comment
	code := "1 + 2\n//3 + 5"

	parser := NewParser()
	parser.SetLanguage(getTestGrammar())
	mainTree := parser.ParseString(nil, []byte(code))
	assert.Equal(
		"(expression (sum left: (expression (number)) right: (expression (number))) (comment))",
		mainTree.RootNode().String(),
	)
	commentNode := mainTree.RootNode().NamedChild(1)
	assert.Equal("comment", commentNode.Type())

	commentRange := Range{
		StartPoint: Point{
			Row:    commentNode.StartPoint().Row,
			Column: commentNode.StartPoint().Column + 2,
		},
		EndPoint:  commentNode.EndPoint(),
		StartByte: commentNode.StartByte() + 2,
		EndByte:   commentNode.EndByte(),
	}

	parser.SetIncludedRanges([]Range{commentRange})
	commentTree := parser.ParseString(nil, []byte(code))

	assert.Equal(
		"(expression (sum left: (expression (number)) right: (expression (number))))",
		commentTree.RootNode().String(),
	)
}

func TestSameNode(t *testing.T) {
	assert := assert.New(t)

	parser := NewParser()
	parser.SetLanguage(getTestGrammar())
	tree := parser.ParseString(nil, []byte("1 + 2"))

	n1 := tree.RootNode()
	n2 := tree.RootNode()

	assert.True(n1 == n2)

	n1 = tree.RootNode().NamedChild(0)
	n2 = tree.RootNode().NamedChild(0)

	assert.True(n1 == n2)
}

func TestQuery(t *testing.T) {
	js := "1 + 2"

	// test single capture
	testCaptures(t, js, "(sum left: (expression) @left)", []string{
		"1",
	})

	// test multiple captures
	testCaptures(t, js, "(sum left: * @left right: * @right)", []string{
		"1",
		"2",
	})

	// test match only
	parser := NewParser()
	parser.SetLanguage(getTestGrammar())
	tree := parser.ParseString(nil, []byte(js))
	root := tree.RootNode()

	q, err := NewQuery([]byte("(sum) (number)"), getTestGrammar())
	assert.Nil(t, err)

	qc := NewQueryCursor()
	qc.Exec(q, root)

	var matched int
	for {
		_, ok := qc.NextMatch()
		if !ok {
			break
		}

		matched++
	}

	assert.Equal(t, 3, matched)
}

func testCaptures(t *testing.T, body, sq string, expected []string) {
	assert := assert.New(t)

	parser := NewParser()
	parser.SetLanguage(getTestGrammar())
	tree := parser.ParseString(nil, []byte(body))
	root := tree.RootNode()

	q, err := NewQuery([]byte(sq), getTestGrammar())
	assert.Nil(err)

	qc := NewQueryCursor()
	qc.Exec(q, root)

	actual := []string{}
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}

		for _, c := range m.Captures {
			actual = append(actual, c.Node.Content([]byte(body)))
		}
	}

	assert.EqualValues(expected, actual)
}

func TestQueryError(t *testing.T) {
	assert := assert.New(t)

	q, err := NewQuery([]byte("((unknown) name: (identifier))"), getTestGrammar())

	assert.Nil(q)
	assert.NotNil(err)
	assert.EqualValues(&QueryError{Offset: 0x02, Type: QueryErrorNodeType}, err)
}

func doWorkLifetime(t testing.TB, n *Node) {
	for i := 0; i < 100; i++ {
		// this will trigger an actual bug (if it still there)
		s := n.String()
		require.Equal(t, "(expression (sum left: (expression (number)) right: (expression (number))))", s)
	}
}

func TestParserLifetime(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				p := NewParser()
				p.SetLanguage(getTestGrammar())
				data := []byte("1 + 2")
				// create some memory/CPU pressure
				data = append(data, bytes.Repeat([]byte(" "), 1024*1024)...)

				root := p.ParseString(nil, data).RootNode()
				// make sure we have no references to the Parser
				p = nil
				// must be a separate function, and it shouldn't accept the parser, only the Tree
				doWorkLifetime(t, root)
			}
		}()
	}
	wg.Wait()
}

func TestTreeCursor(t *testing.T) {
	assert := assert.New(t)

	input := []byte(`1 + 2`)

	root := Parse(input, getTestGrammar())
	c := NewTreeCursor(root)

	assert.True(c.CurrentNode() == root)
	assert.Equal("", c.CurrentFieldName())

	assert.False(c.GoToParent())
	assert.False(c.GoToNextSibling())
	assert.Equal(int64(-1), c.GoToFirstChildForByte(100))

	assert.True(c.GoToFirstChild())
	assert.Equal("sum", c.CurrentNode().Type())
	assert.True(c.GoToFirstChild())
	assert.Equal("expression", c.CurrentNode().Type())
	assert.Equal("left", c.CurrentFieldName())
	assert.True(c.GoToNextSibling())
	assert.Equal("+", c.CurrentNode().Type())
	assert.False(c.GoToFirstChild())
	assert.True(c.GoToNextSibling())
	assert.Equal("expression", c.CurrentNode().Type())
	assert.True(c.GoToFirstChild())
	assert.Equal("number", c.CurrentNode().Type())

	assert.True(c.GoToParent())
	assert.True(c.GoToParent())
	assert.Equal("sum", c.CurrentNode().Type())
	nodeForReset := c.CurrentNode()

	assert.Equal(int64(2), c.GoToFirstChildForByte(3))
	assert.Equal("expression", c.CurrentNode().Type())

	c.Reset(nodeForReset)
	assert.Equal("sum", c.CurrentNode().Type())
	assert.False(c.GoToParent())
}
