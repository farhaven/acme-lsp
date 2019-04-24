package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"9fans.net/go/acme"
	"9fans.net/go/plan9"
	p9client "9fans.net/go/plan9/client"
	"9fans.net/go/plumb"
	"github.com/fhs/acme-lsp/internal/acmeutil"
	"github.com/fhs/acme-lsp/internal/lsp"
	"github.com/fhs/acme-lsp/internal/lsp/client"
	"github.com/fhs/acme-lsp/internal/lsp/text"
	"github.com/pkg/errors"
)

//go:generate ./mkdocs.sh

const mainDoc = `The program L is a client for the acme text editor that interacts with a
Language Server.

A Language Server implements the Language Server Protocol
(see https://langserver.org/), which provides language features
like auto complete, go to definition, find all references, etc.
L depends on one or more language servers already being installed
in the system.  See this page of a list of language servers:
https://microsoft.github.io/language-server-protocol/implementors/servers/.

	Usage: L [flags] <sub-command> [args...]

List of sub-commands:

	comp
		Show auto-completion for the current cursor position.

	def
		Find where identifier at the cursor position is define and
		send the location to the plumber.

	fmt
		Format current window buffer.

	hov
		Show more information about the identifier under the cursor
		("hover").

	monitor
		Format window buffer after each Put.

	refs
		List locations where the identifier under the cursor is used
		("references").

	rn <newname>
		Rename the identifier under the cursor to newname.

	servers
		Print list of known language servers.

	sig
		Show signature help for the function, method, etc. under
		the cursor.

	syms
		List symbols in the current file.

	win <command>
		The command argument can be either "comp", "hov" or "sig". A
		new window is created where the output of the given command
		is shown each time cursor position is changed.
`

var debug = flag.Bool("debug", false, "turn on debugging prints")
var workspaces = flag.String("workspaces", "", "colon-separated list of initial workspace directories")
var userServers serverFlag
var dialServers serverFlag

var serverSet client.ServerSet

func usage() {
	os.Stderr.Write([]byte(mainDoc))
	fmt.Fprintf(os.Stderr, "\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	flag.Usage = usage
	flag.Var(&userServers, "server", `language server command for filename match (e.g. '\.go$:gopls')`)
	flag.Var(&dialServers, "dial", `language server address for filename match (e.g. '\.go$:localhost:4389')`)
	flag.Parse()

	if *debug {
		client.Debug = true
	}

	initServerSet()

	if flag.NArg() < 1 {
		usage()
	}
	switch flag.Arg(0) {
	case "win":
		if flag.NArg() < 2 {
			usage()
		}
		watch(flag.Arg(1))
		serverSet.CloseAll()
		return

	case "monitor":
		monitor()
		serverSet.CloseAll()
		return

	case "servers":
		serverSet.PrintTo(os.Stdout)
		return
	}
	w, err := acmeutil.OpenCurrentWin()
	if err != nil {
		log.Fatalf("failed to to open current window: %v\n", err)
	}
	defer w.CloseFiles()
	pos, fname, err := text.Position(w)
	if err != nil {
		log.Fatal(err)
	}
	s, err := serverSet.StartForFile(fname)
	if err != nil {
		log.Fatalf("cound not start language server: %v\n", err)
	}
	defer s.Close()

	b, err := w.ReadAll("body")
	if err != nil {
		log.Fatalf("failed to read source body: %v\n", err)
	}
	if err = s.Conn.DidOpen(fname, b); err != nil {
		log.Fatalf("DidOpen failed: %v\n", err)
	}
	defer func() {
		if err = s.Conn.DidClose(fname); err != nil {
			log.Printf("DidClose failed: %v\n", err)
		}
	}()

	switch flag.Arg(0) {
	case "comp":
		err = s.Conn.Completion(pos, os.Stdout)
	case "def":
		err = plumbDefinition(s.Conn, pos)
	case "fmt":
		err = formatInEditor(s.Conn, pos.TextDocument.URI, w)
	case "hov":
		err = s.Conn.Hover(pos, os.Stdout)
	case "refs":
		err = s.Conn.References(pos, os.Stdout)
	case "rn":
		if flag.NArg() < 2 {
			usage()
		}
		err = renameInEditor(s.Conn, pos, flag.Arg(1))
	case "sig":
		err = s.Conn.SignatureHelp(pos, os.Stdout)
	case "syms":
		err = s.Conn.Symbols(pos.TextDocument.URI, os.Stdout)
	default:
		log.Printf("unknown command %q\n", flag.Arg(0))
		os.Exit(1)
	}
	if err != nil {
		log.Fatalf("%v\n", err)
	}
}

func plumbDefinition(c *client.Conn, pos *lsp.TextDocumentPositionParams) error {
	p, err := plumb.Open("send", plan9.OWRITE)
	if err != nil {
		return errors.Wrap(err, "failed to open plumber")
	}
	defer p.Close()
	locations, err := c.Definition(pos)
	if err != nil {
		return err
	}
	for _, loc := range locations {
		err := plumbLocation(p, &loc)
		if err != nil {
			return errors.Wrap(err, "failed to plumb location")
		}
	}
	return nil
}

func plumbLocation(p *p9client.Fid, loc *lsp.Location) error {
	fn := text.ToPath(loc.URI)
	a := fmt.Sprintf("%v:%v", fn, loc.Range.Start)

	m := &plumb.Message{
		Src:  "L",
		Dst:  "edit",
		Dir:  "/",
		Type: "text",
		Data: []byte(a),
	}
	return m.Send(p)
}

func formatWin(id int) error {
	w, err := acmeutil.OpenWin(id)
	if err != nil {
		return err
	}
	uri, fname, err := text.DocumentURI(w)
	if err != nil {
		return err
	}
	s, err := serverSet.StartForFile(fname)
	if err != nil {
		return nil // unknown language server
	}
	b, err := w.ReadAll("body")
	if err != nil {
		log.Fatalf("failed to read source body: %v\n", err)
	}
	if err := s.Conn.DidOpen(fname, b); err != nil {
		log.Fatalf("DidOpen failed: %v\n", err)
	}
	defer func() {
		if err := s.Conn.DidClose(fname); err != nil {
			log.Printf("DidClose failed: %v\n", err)
		}
	}()
	return formatInEditor(s.Conn, uri, w)
}

func formatInEditor(c *client.Conn, uri lsp.DocumentURI, f text.File) error {
	edits, err := c.Format(uri)
	if err != nil {
		return err
	}
	if err := text.Edit(f, edits); err != nil {
		return errors.Wrapf(err, "failed to apply edits")
	}
	return nil
}

func renameInEditor(c *client.Conn, pos *lsp.TextDocumentPositionParams, newname string) error {
	we, err := c.Rename(pos, newname)
	if err != nil {
		return err
	}
	return editWorkspace(we)
}

func editWorkspace(we *lsp.WorkspaceEdit) error {
	wins, err := acme.Windows()
	if err != nil {
		return errors.Wrapf(err, "failed to read list of acme index")
	}
	winid := make(map[string]int, len(wins))
	for _, info := range wins {
		winid[info.Name] = info.ID
	}

	for uri := range we.Changes {
		fname := text.ToPath(uri)
		if _, ok := winid[fname]; !ok {
			return fmt.Errorf("%v: not open in acme", fname)
		}
	}
	for uri, edits := range we.Changes {
		fname := text.ToPath(uri)
		id := winid[fname]
		w, err := acmeutil.OpenWin(id)
		if err != nil {
			return errors.Wrapf(err, "failed to open window %v", id)
		}
		if err := text.Edit(w, edits); err != nil {
			return errors.Wrapf(err, "failed to apply edits to window %v", id)
		}
		w.CloseFiles()
	}
	return nil
}

func monitor() {
	alog, err := acme.Log()
	if err != nil {
		panic(err)
	}
	defer alog.Close()
	for {
		ev, err := alog.Read()
		if err != nil {
			panic(err)
		}
		if ev.Op == "put" {
			if err = formatWin(ev.ID); err != nil {
				log.Printf("formating window %v failed: %v\n", ev.ID, err)
			}
		}
	}
}

func initServerSet() {
	if len(*workspaces) > 0 {
		serverSet.Workspaces = strings.Split(*workspaces, ":")
	}
	// golang.org/x/tools/cmd/gopls is not ready. It hasn't implmented
	// references, and rename yet.
	//serverSet.Register(`\.go$`, []string{"gopls"})
	serverSet.Register(`\.go$`, []string{"go-langserver", "-gocodecompletion"})
	serverSet.Register(`\.py$`, []string{"pyls"})
	//serverSet.Register(`\.c$`, []string{"cquery"})

	if len(userServers) > 0 {
		for _, sa := range userServers {
			serverSet.Register(sa.pattern, strings.Fields(sa.args))
		}
	}
	if len(dialServers) > 0 {
		for _, sa := range userServers {
			serverSet.RegisterDial(sa.pattern, sa.args)
		}
	}
}

type serverArgs struct {
	pattern, args string
}

type serverFlag []serverArgs

func (sf *serverFlag) String() string {
	return fmt.Sprintf("%v", []serverArgs(*sf))
}

func (sf *serverFlag) Set(val string) error {
	f := strings.SplitN(val, ":", 2)
	if len(f) != 2 {
		return errors.New("bad flag value")
	}
	*sf = append(*sf, serverArgs{
		pattern: f[0],
		args:    f[1],
	})
	return nil
}
