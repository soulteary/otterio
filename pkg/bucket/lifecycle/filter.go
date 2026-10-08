/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package lifecycle

import (
	"encoding/xml"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"
)

var (
	errInvalidFilter = Errorf("Filter must have exactly one of Prefix, Tag, or And specified")
)

// Filter - a filter for a lifecycle configuration Rule.
type Filter struct {
	XMLName xml.Name `xml:"Filter"`
	set     bool

	Prefix Prefix

	And    And
	andSet bool

	Tag    Tag
	tagSet bool
}

// MarshalXML - produces the xml representation of the Filter struct
// only one of Prefix, And and Tag should be present in the output.
func (f Filter) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := e.EncodeToken(start); err != nil {
		return err
	}

	switch {
	case !f.And.isEmpty():
		if err := e.EncodeElement(f.And, xml.StartElement{Name: xml.Name{Local: "And"}}); err != nil {
			return err
		}
	case !f.Tag.IsEmpty():
		if err := e.EncodeElement(f.Tag, xml.StartElement{Name: xml.Name{Local: "Tag"}}); err != nil {
			return err
		}
	default:
		// Always print Prefix field when both And & Tag are empty
		if err := e.EncodeElement(f.Prefix, xml.StartElement{Name: xml.Name{Local: "Prefix"}}); err != nil {
			return err
		}
	}

	return e.EncodeToken(xml.EndElement{Name: start.Name})
}

// UnmarshalXML - decodes XML data.
func (f *Filter) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) (err error) {
	f.set = true
	for {
		// Read tokens from the XML document in a stream.
		t, err := d.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		switch se := t.(type) {
		case xml.StartElement:
			switch se.Name.Local {
			case "Prefix":
				var p Prefix
				if err = d.DecodeElement(&p, &se); err != nil {
					return err
				}
				f.Prefix = p
			case "And":
				var and And
				if err = d.DecodeElement(&and, &se); err != nil {
					return err
				}
				f.And = and
				f.andSet = true
			case "Tag":
				var tag Tag
				if err = d.DecodeElement(&tag, &se); err != nil {
					return err
				}
				f.Tag = tag
				f.tagSet = true
			default:
				return errUnknownXMLTag
			}
		}
	}
	return nil
}

// IsEmpty returns true if Filter is not specified in the XML
func (f Filter) IsEmpty() bool {
	return !f.set
}

// Validate - validates the filter element
func (f Filter) Validate() error {
	if f.IsEmpty() {
		return errXMLNotWellFormed
	}
	// A Filter must have exactly one of Prefix, Tag, or And specified.
	if !f.And.isEmpty() {
		if f.Prefix.set {
			return errInvalidFilter
		}
		if !f.Tag.IsEmpty() {
			return errInvalidFilter
		}
		if err := f.And.Validate(); err != nil {
			return err
		}
	}
	if f.Prefix.set {
		if !f.Tag.IsEmpty() {
			return errInvalidFilter
		}
	}
	if !f.Tag.IsEmpty() {
		if f.Prefix.set {
			return errInvalidFilter
		}
		if err := f.Tag.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// TestTags tests if the object tags satisfy the Filter tags requirement,
// it returns true if there is no tags in the underlying Filter.
func (f Filter) TestTags(tags []string) bool {
	if len(f.And.Tags) == 0 && f.Tag.IsEmpty() {
		return true
	}
	for _, tag := range tags {
		if !strings.Contains(tag, "=") {
			return false
		}
	}
	// Object tags use the URL-encoded x-amz-tagging representation. Decode
	// keys/values instead of comparing an encoded pair with a raw XML value.
	// Refuse malformed or ambiguous input rather than broadening a filter.
	values, err := url.ParseQuery(strings.Join(tags, "&"))
	if err != nil {
		return false
	}
	for key, value := range values {
		if key == "" || !utf8.ValidString(key) || len(value) != 1 || !utf8.ValidString(value[0]) {
			return false
		}
	}
	matches := func(tag Tag) bool {
		if tag.IsEmpty() {
			return true
		}
		value, found := values[tag.Key]
		return found && value[0] == tag.Value
	}
	for _, tag := range f.And.Tags {
		if !matches(tag) {
			return false
		}
	}
	return matches(f.Tag)
}
