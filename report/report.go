package report

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/alecthomas/participle/lexer"
	"github.com/logrusorgru/aurora"
	"github.com/openllb/hlb/parser"
)

var (
	Sources = []string{"scratch", "image", "http", "git", "local", "generate"}
	Ops     = []string{"shell", "run", "env", "dir", "user", "entrypoint", "mkdir", "mkfile", "rm", "copy"}
	Debugs  = []string{"breakpoint"}
	Types   = []string{"string", "int", "bool", "fs", "option"}

	CommonOptions   = []string{"no-cache"}
	ImageOptions    = []string{"resolve"}
	HTTPOptions     = []string{"checksum", "chmod", "filename"}
	GitOptions      = []string{"keepGitDir"}
	LocalOptions    = []string{"includePatterns", "excludePatterns", "followPaths"}
	GenerateOptions = []string{"frontendInput", "frontendOpt"}
	RunOptions      = []string{"readonlyRootfs", "env", "dir", "user", "network", "security", "host", "ssh", "secret", "mount"}
	SSHOptions      = []string{"target", "id", "uid", "gid", "mode", "optional"}
	SecretOptions   = []string{"id", "uid", "gid", "mode", "optional"}
	MountOptions    = []string{"readonly", "tmpfs", "sourcePath", "cache"}
	MkdirOptions    = []string{"createParents", "chown", "createdTime"}
	MkfileOptions   = []string{"chown", "createdTime"}
	RmOptions       = []string{"allowNotFound", "allowWildcard"}
	CopyOptions     = []string{"followSymlinks", "contentsOnly", "unpack", "createDestPath", "allowWildcard", "allowEmptyWildcard", "chown", "createdTime"}

	NetworkModes      = []string{"unset", "host", "none"}
	SecurityModes     = []string{"sandbox", "insecure"}
	CacheSharingModes = []string{"shared", "private", "locked"}

	Options          = flatMap(ImageOptions, HTTPOptions, GitOptions, RunOptions, SSHOptions, SecretOptions, MountOptions, MkdirOptions, MkfileOptions, RmOptions, CopyOptions)
	Enums            = flatMap(NetworkModes, SecurityModes, CacheSharingModes)
	Fields           = flatMap(Sources, Ops, Options)
	Keywords         = flatMap(Types, Sources, Fields, Enums)
	ReservedKeywords = flatMap(Types, []string{"with"})

	KeywordsWithOptions = []string{"image", "http", "git", "run", "ssh", "secret", "mount", "mkdir", "mkfile", "rm", "copy"}
	KeywordsWithBlocks  = flatMap(Types, KeywordsWithOptions)

	KeywordsByName = map[string][]string{
		"fs":       Ops,
		"image":    flatMap(CommonOptions, ImageOptions),
		"http":     flatMap(CommonOptions, HTTPOptions),
		"git":      flatMap(CommonOptions, GitOptions),
		"local":    flatMap(CommonOptions, LocalOptions),
		"generate": flatMap(CommonOptions, GenerateOptions),
		"run":      flatMap(CommonOptions, RunOptions),
		"ssh":      flatMap(CommonOptions, SSHOptions),
		"secret":   flatMap(CommonOptions, SecretOptions),
		"mount":    flatMap(CommonOptions, MountOptions),
		"mkdir":    flatMap(CommonOptions, MkdirOptions),
		"mkfile":   flatMap(CommonOptions, MkfileOptions),
		"rm":       flatMap(CommonOptions, RmOptions),
		"copy":     flatMap(CommonOptions, CopyOptions),
		"network":  NetworkModes,
		"security": SecurityModes,
		"cache":    CacheSharingModes,
	}

	BuiltinSources = map[parser.ObjType][]string{
		parser.Filesystem: Sources,
		parser.Str:        []string{"value", "format"},
	}

	Builtins = map[parser.ObjType]map[string][]*parser.Field{
		parser.Filesystem: map[string][]*parser.Field{
			// Debug ops
			"breakpoint": nil,
			// Source ops
			"scratch": nil,
			"image": []*parser.Field{
				parser.NewField(parser.Str, "ref", false),
			},
			"http": []*parser.Field{
				parser.NewField(parser.Str, "url", false),
			},
			"git": []*parser.Field{
				parser.NewField(parser.Str, "remote", false),
				parser.NewField(parser.Str, "ref", false),
			},
			"local": []*parser.Field{
				parser.NewField(parser.Str, "path", false),
			},
			"generate": []*parser.Field{
				parser.NewField(parser.Filesystem, "frontend", false),
			},
			// Ops
			"shell": []*parser.Field{
				parser.NewField(parser.Str, "arg", true),
			},
			"run": []*parser.Field{
				parser.NewField(parser.Str, "arg", true),
			},
			"env": []*parser.Field{
				parser.NewField(parser.Str, "key", false),
				parser.NewField(parser.Str, "value", false),
			},
			"dir": []*parser.Field{
				parser.NewField(parser.Str, "path", false),
			},
			"user": []*parser.Field{
				parser.NewField(parser.Str, "name", false),
			},
			"entrypoint": []*parser.Field{
				parser.NewField(parser.Str, "command", true),
			},
			"mkdir": []*parser.Field{
				parser.NewField(parser.Str, "path", false),
				parser.NewField(parser.Int, "filemode", false),
			},
			"mkfile": []*parser.Field{
				parser.NewField(parser.Str, "path", false),
				parser.NewField(parser.Int, "filemode", false),
				parser.NewField(parser.Str, "content", false),
			},
			"rm": []*parser.Field{
				parser.NewField(parser.Str, "path", false),
			},
			"copy": []*parser.Field{
				parser.NewField(parser.Filesystem, "input", false),
				parser.NewField(parser.Str, "src", false),
				parser.NewField(parser.Str, "dest", false),
			},
		},
		parser.Str: map[string][]*parser.Field{
			"value": []*parser.Field{
				parser.NewField(parser.Str, "literal", false),
			},
			"format": []*parser.Field{
				parser.NewField(parser.Str, "format", false),
				parser.NewField(parser.Str, "values", true),
			},
		},
		// Common options
		parser.Option: map[string][]*parser.Field{
			"no-cache": nil,
		},
		parser.OptionImage: map[string][]*parser.Field{
			"resolve": nil,
		},
		parser.OptionHTTP: map[string][]*parser.Field{
			"checksum": []*parser.Field{
				parser.NewField(parser.Str, "digest", false),
			},
			"chmod": []*parser.Field{
				parser.NewField(parser.Int, "filemode", false),
			},
			"filename": []*parser.Field{
				parser.NewField(parser.Str, "name", false),
			},
		},
		parser.OptionGit: map[string][]*parser.Field{
			"keepGitDir": nil,
		},
		parser.OptionLocal: map[string][]*parser.Field{
			"includePatterns": []*parser.Field{
				parser.NewField(parser.Str, "patterns", true),
			},
			"excludePatterns": []*parser.Field{
				parser.NewField(parser.Str, "patterns", true),
			},
			"followPaths": []*parser.Field{
				parser.NewField(parser.Str, "paths", true),
			},
		},
		parser.OptionGenerate: map[string][]*parser.Field{
			"frontendInput": []*parser.Field{
				parser.NewField(parser.Str, "key", false),
				parser.NewField(parser.Filesystem, "value", false),
			},
			"frontendOpt": []*parser.Field{
				parser.NewField(parser.Str, "key", false),
				parser.NewField(parser.Str, "value", false),
			},
		},
		parser.OptionRun: map[string][]*parser.Field{
			"readonlyRootfs": nil,
			"env": []*parser.Field{
				parser.NewField(parser.Str, "key", false),
				parser.NewField(parser.Str, "value", false),
			},
			"dir": []*parser.Field{
				parser.NewField(parser.Str, "path", false),
			},
			"user": []*parser.Field{
				parser.NewField(parser.Str, "name", false),
			},
			"network": []*parser.Field{
				parser.NewField(parser.Str, "networkmode", false),
			},
			"security": []*parser.Field{
				parser.NewField(parser.Str, "securitymode", false),
			},
			"host": []*parser.Field{
				parser.NewField(parser.Str, "hostname", false),
				parser.NewField(parser.Str, "address", false),
			},
			"ssh": nil,
			"secret": []*parser.Field{
				parser.NewField(parser.Str, "mountpoint", false),
			},
			"mount": []*parser.Field{
				parser.NewField(parser.Filesystem, "input", false),
				parser.NewField(parser.Str, "mountpoint", false),
			},
		},
		parser.OptionSSH: map[string][]*parser.Field{
			"target": []*parser.Field{
				parser.NewField(parser.Str, "path", false),
			},
			"id": []*parser.Field{
				parser.NewField(parser.Str, "cacheid", false),
			},
			"uid": []*parser.Field{
				parser.NewField(parser.Int, "value", false),
			},
			"gid": []*parser.Field{
				parser.NewField(parser.Int, "value", false),
			},
			"mode": []*parser.Field{
				parser.NewField(parser.Int, "filemode", false),
			},
			"optional": nil,
		},
		parser.OptionSecret: map[string][]*parser.Field{
			"id": []*parser.Field{
				parser.NewField(parser.Str, "cacheid", false),
			},
			"uid": []*parser.Field{
				parser.NewField(parser.Int, "value", false),
			},
			"gid": []*parser.Field{
				parser.NewField(parser.Int, "value", false),
			},
			"mode": []*parser.Field{
				parser.NewField(parser.Int, "filemode", false),
			},
			"optional": nil,
		},
		parser.OptionMount: map[string][]*parser.Field{
			"readonly": nil,
			"tmpfs":    nil,
			"sourcePath": []*parser.Field{
				parser.NewField(parser.Str, "path", false),
			},
			"cache": []*parser.Field{
				parser.NewField(parser.Str, "cacheid", false),
				parser.NewField(parser.Str, "cachemode", false),
			},
		},
		parser.OptionMkdir: map[string][]*parser.Field{
			"createParents": nil,
			"chown": []*parser.Field{
				parser.NewField(parser.Str, "owner", false),
			},
			"createdTime": []*parser.Field{
				parser.NewField(parser.Str, "created", false),
			},
		},
		parser.OptionMkfile: map[string][]*parser.Field{
			"chown": []*parser.Field{
				parser.NewField(parser.Str, "owner", false),
			},
			"createdTime": []*parser.Field{
				parser.NewField(parser.Str, "created", false),
			},
		},
		parser.OptionRm: map[string][]*parser.Field{
			"allowNotFound":  nil,
			"allowWildcards": nil,
		},
		parser.OptionCopy: map[string][]*parser.Field{
			"followSymlinks": nil,
			"contentsOnly":   nil,
			"unpack":         nil,
			"createDestPath": nil,
		},
	}
)

func flatMap(arrays ...[]string) []string {
	set := make(map[string]struct{})
	var flat []string
	for _, array := range arrays {
		for _, elem := range array {
			if _, ok := set[elem]; ok {
				continue
			}
			flat = append(flat, elem)
			set[elem] = struct{}{}
		}
	}
	return flat
}

func keys(m map[string][]*parser.Field) []string {
	var keys []string
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

type Error struct {
	Groups []AnnotationGroup
}

func (e Error) Error() string {
	var lines []string
	for _, group := range e.Groups {
		lines = append(lines, group.String())
	}

	return fmt.Sprintf("%s", strings.Join(lines, "\n"))
}

type AnnotationGroup struct {
	Color       aurora.Aurora
	Pos         lexer.Position
	Annotations []Annotation
	Help        string
}

func (ag AnnotationGroup) String() string {
	maxLn := 0
	for _, an := range ag.Annotations {
		ln := fmt.Sprintf("%d", an.Pos.Line)
		if len(ln) > maxLn {
			maxLn = len(ln)
		}
	}

	var annotations []string
	for _, an := range ag.Annotations {
		var lines []string
		for i, line := range an.Lines(ag.Color) {
			var ln string
			if i == 1 {
				ln = fmt.Sprintf("%d", an.Pos.Line)
			}

			prefix := ag.Color.Sprintf(ag.Color.Blue("%s%s | "), ln, strings.Repeat(" ", maxLn-len(ln)))
			lines = append(lines, fmt.Sprintf("%s%s", prefix, line))
		}
		annotations = append(annotations, strings.Join(lines, "\n"))
	}

	gutter := strings.Repeat(" ", maxLn)
	header := fmt.Sprintf(
		"%s %s",
		ag.Color.Sprintf(ag.Color.Blue("%s-->"), gutter),
		ag.Color.Sprintf(ag.Color.Bold("%s:%d:%d: syntax error"), ag.Pos.Filename, ag.Pos.Line, ag.Pos.Column))
	body := strings.Join(annotations, ag.Color.Sprintf(ag.Color.Blue("\n%s ⫶\n"), gutter))

	var footer string
	if ag.Help != "" {
		footer = fmt.Sprintf(
			"%s%s%s",
			ag.Color.Sprintf(ag.Color.Blue("\n%s | \n"), gutter),
			ag.Color.Sprintf(ag.Color.Green("%s[?] help: "), gutter),
			ag.Help)
	}

	return fmt.Sprintf("%s\n%s%s\n", header, body, footer)
}

type Annotation struct {
	Pos     lexer.Position
	Token   lexer.Token
	Segment []byte
	Message string
}

func (a Annotation) Lines(color aurora.Aurora) []string {
	end := a.Pos.Column - 1
	if len(a.Segment) <= a.Pos.Column-1 {
		end = len(a.Segment) - len("⏎") - 1
	}

	var padding []byte
	if !a.Token.EOF() {
		padding = bytes.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return r
			}
			return ' '
		}, a.Segment[:end])
	}

	underline := len(a.Token.String())
	if isSymbol(a.Token, "Newline") {
		underline = 1
	} else if isSymbol(a.Token, "String") {
		underline += 2
	}

	var lines []string
	lines = append(lines, "")
	lines = append(lines, string(a.Segment))
	lines = append(lines, color.Sprintf(color.Red("%s%s"), padding, strings.Repeat("^", underline)))
	lines = append(lines, fmt.Sprintf("%s%s", padding, a.Message))

	return lines
}

type IndexedBuffer struct {
	buf     *bytes.Buffer
	offset  int
	offsets []int
}

func NewIndexedBuffer() *IndexedBuffer {
	return &IndexedBuffer{
		buf: new(bytes.Buffer),
	}
}

func (ib *IndexedBuffer) Len() int {
	return len(ib.offsets)
}

func (ib *IndexedBuffer) Write(p []byte) (n int, err error) {
	n, err = ib.buf.Write(p)

	start := 0
	index := bytes.IndexByte(p[:n], byte('\n'))
	for index >= 0 {
		ib.offsets = append(ib.offsets, ib.offset+start+index)
		start += index + 1
		index = bytes.IndexByte(p[start:n], byte('\n'))
	}
	ib.offset += n

	return n, err
}

func (ib *IndexedBuffer) Segment(offset int) ([]byte, error) {
	if len(ib.offsets) == 0 {
		return ib.buf.Bytes(), nil
	}

	index := ib.findNearestLineIndex(offset)

	start := 0
	if index >= 0 {
		start = ib.offsets[index] + 1
	}

	if start > ib.buf.Len()-1 {
		return nil, io.EOF
	}

	var end int
	if offset < ib.offsets[len(ib.offsets)-1] {
		end = ib.offsets[index+1]
	} else {
		end = ib.buf.Len()
	}

	return ib.read(start, end)
}

func (ib *IndexedBuffer) Line(num int) ([]byte, error) {
	if num > len(ib.offsets) {
		return nil, fmt.Errorf("line %d outside of offsets", num)
	}

	start := 0
	if num > 0 {
		start = ib.offsets[num-1] + 1
	}

	end := ib.offsets[0]
	if num > 0 {
		end = ib.offsets[num]
	}

	return ib.read(start, end)
}

func (ib *IndexedBuffer) findNearestLineIndex(offset int) int {
	index := sort.Search(len(ib.offsets), func(i int) bool {
		return ib.offsets[i] >= offset
	})

	if index < len(ib.offsets) {
		if ib.offsets[index] < offset {
			return index
		}
		return index - 1
	} else {
		// If offset is further than any newline, then the last newline is the
		// nearest.
		return index - 1
	}
}

func (ib *IndexedBuffer) read(start, end int) ([]byte, error) {
	r := bytes.NewReader(ib.buf.Bytes())

	_, err := r.Seek(int64(start), io.SeekStart)
	if err != nil {
		return nil, err
	}

	line := make([]byte, end-start)
	n, err := r.Read(line)
	if err != nil && err != io.EOF {
		return nil, err
	}

	return line[:n], nil
}
