package torznab

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// rootError is the root element of the Newznab/Torznab error document,
// which those servers return with HTTP 200 in place of the response that was
// asked for.
const rootError = "error"

// decodeDocument parses one API response into into.
//
// It reads the root element itself rather than handing the whole body to
// xml.Unmarshal, because three different documents arrive on the same
// endpoint and only one of them is the one that was asked for: the response
// proper, the <error> document (which is a *successful* HTTP response
// carrying a failure), and whatever a misconfigured or fronted server
// returns instead — an HTML sign-in page being the usual one.
//
// No text from the document ever reaches the error. See xmlFailure and
// describeRoot for why that is a rule rather than an accident.
func decodeDocument(body []byte, root string, into any) error {
	dec := xml.NewDecoder(bytes.NewReader(body))

	for {
		token, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ErrDocumentEmpty
			}

			return xmlFailure(err)
		}

		start, ok := token.(xml.StartElement)
		if !ok {
			// A declaration, a doctype, a comment, or whitespace
			// before the root element. Keep looking.
			continue
		}

		name := strings.ToLower(start.Name.Local)

		switch name {
		case root:
			if err := dec.DecodeElement(into, &start); err != nil {
				return xmlFailure(err)
			}

			return nil
		case rootError:
			return decodeAPIError(dec, &start)
		default:
			return fmt.Errorf("%w (its root element is %s, not <%s>)",
				ErrDocumentUnexpectedRoot, describeRoot(name), root)
		}
	}
}

// decodeAPIError reads a Newznab/Torznab <error> document.
//
// The struct it decodes into has one field, and that is the point: the
// document's description attribute is never read, never stored, and never
// reaches memory this package owns. Both major implementations put
// unfiltered internal text in it — Jackett passes a whole .NET exception
// (`GetErrorXML(900, e.ToString())` in src/Jackett.Server/Controllers/
// ResultsController.cs, branch master) and Prowlarr passes `ex.Message`
// (src/Prowlarr.Api.V1/Indexers/NewznabController.cs, branch develop) —
// and every request tortui makes carries the user's api_key in its query
// string. A description that echoed the request back would put that key
// wherever the error went (DEC-066).
func decodeAPIError(dec *xml.Decoder, start *xml.StartElement) error {
	var doc struct {
		Code string `xml:"code,attr"`
	}

	if err := dec.DecodeElement(&doc, start); err != nil {
		return xmlFailure(err)
	}

	code, err := strconv.Atoi(strings.TrimSpace(doc.Code))
	if err != nil {
		// A code this package cannot read is reported as an
		// unspecified one rather than as a parse failure: the server
		// still said "this request failed", which is the part that
		// matters.
		code = 0
	}

	return &APIError{Code: code}
}

// xmlFailure turns an encoding/xml failure into an error that says what went
// wrong without quoting the document.
//
// A *xml.SyntaxError's message is built partly from the document's own
// content — element and entity names appear in it verbatim — and the
// document is written by the source, which knows the user's api_key because
// every request carries it. So the line number is reported and the message
// is not. The line is enough to tell a truncated feed from a broken one, and
// nothing the source wrote can travel in an integer (DEC-066).
func xmlFailure(err error) error {
	var syntaxErr *xml.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Errorf("%w (xml syntax error on line %d)", ErrDocumentMalformed, syntaxErr.Line)
	}

	return fmt.Errorf("%w (%T from encoding/xml)", ErrDocumentMalformed, err)
}

// describeRoot names the root element of a document that is not the one that
// was asked for.
//
// Only the handful of names tortui already knows are repeated back. An
// element name is document content — a hostile source could name its root
// after the api_key it was sent — so an unrecognised one is described rather
// than quoted (DEC-066).
func describeRoot(name string) string {
	switch name {
	case rootFeed, rootCaps, rootError, "html":
		return "<" + name + ">"
	default:
		return "an unrecognised element"
	}
}
