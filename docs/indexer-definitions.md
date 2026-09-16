# Writing an indexer definition

tortui talks to a site it has never heard of by reading a **definition**: a YAML
file you write, describing where to send a search and which parts of the page
hold which piece of a result. There is no selector, no endpoint and no site name
compiled into tortui — every scraped source is a file like the one below, and
fixing one when a site changes its markup is a text edit rather than a new
release.

> **Sources are yours.** tortui ships no definition, endpoint, default or preset
> for any source whose primary use is distributing infringing content, and it
> never works around a site's authentication, captcha, paywall or any other
> access control. You are responsible for what you search for and download, for
> complying with the terms of any source you configure, and for the law where
> you are. See `AGENT.md` §2 and §16.

> **A definition never contains a credential.** There is no placeholder for an
> API key, a cookie or a passkey, and a definition that names one fails
> validation. Credentials you obtained from your own account on a site go in
> your own config file; tortui injects them into every request it makes to that
> source. Keeping them out of the definition is what makes a definition a file
> you can read, edit and hand to someone else.

---

## The shape of a file

```yaml
id:            # required, stable identifier for this source
name:          # optional, what the TUI shows; defaults to the id
base_url:      # required, absolute http or https address
mode:          # html (default) or json
requires_auth: # optional, true if the site needs credentials you supply

rows:          # default selector for one result row
fields:        # default field selectors, shared by every block
trust:         # default badge mapping

search:        # required: the keyword search request
latest:        # optional: the recent-additions request
```

`rows`, `fields` and `trust` at the top level are **shared defaults**. A block
that does not set them inherits them; a block that does overrides them. `fields`
is merged key by key, so a block can replace one selector and keep the rest.

### Top level

| Key | Required | Meaning |
|---|---|---|
| `id` | yes | Stable identifier. It is the config key, the registry key, and the source column in the results table. |
| `name` | no | Display name. Defaults to `id`. |
| `base_url` | yes | Absolute `http`/`https` address. Every request and every relative link on the page is resolved against it. |
| `mode` | no | `html` (default) reads CSS selectors with goquery; `json` reads path expressions. |
| `requires_auth` | no | `true` if the site needs credentials you supplied from your own account. It is a display and capability flag only. |
| `rows` | no* | Default row selector. Required somewhere — on the top level or on every block. |
| `fields` | no* | Default field selectors. A `title` field and at least one of `magnet`, `torrent_url` or `infohash` must be in force for every block. |
| `trust` | no | Default badge mapping. |
| `search` | yes | The keyword search request. |
| `latest` | no | The recent-additions request. **Omit it and tortui reports that this source has no latest feed**, skips it for a Latest query, and says so — rather than sending a request that cannot work. |

### A block (`search`, `latest`)

| Key | Required | Meaning |
|---|---|---|
| `path` | no | Appended to `base_url`. May contain placeholders. A leading `/` replaces the base's path; anything else extends it. Empty requests `base_url` itself. |
| `params` | no | Query parameters, as templates. A parameter whose placeholder has nothing to put in it is **not sent at all**. |
| `rows` | no | Overrides the shared row selector for this block. |
| `fields` | no | Overrides shared fields, key by key. |
| `trust` | no | Overrides the shared badge mapping, wholesale. |

#### Placeholders

Only three, and a `{{...}}` that is not one of them is a validation error rather
than something sent to the site verbatim:

| Placeholder | Substituted with |
|---|---|
| `{{query}}` | the keyword the user typed (empty for a latest request) |
| `{{limit}}` | how many results were asked for, when the user asked for a number |
| `{{offset}}` | how many to skip, when paging |

A block whose `path` or `params` mention `{{offset}}` is treated as supporting
pagination; one that does not, is not.

### A field

Every field is optional except `title`. A selector that matches nothing produces
the **zero value** for that field — never an error, and never a dropped result.
A row with no title is skipped, which is how header rows, spacer rows and
between-result advertisements take care of themselves.

| Key | Meaning |
|---|---|
| `selector` | CSS selector (`html` mode) or path expression (`json` mode). Empty means the row itself. |
| `attr` | Read this HTML attribute instead of the element's text. Not allowed in `json` mode. |
| `text` | `true` asks explicitly for the element's text, which is the default. Setting it together with `attr` is an error. |
| `regex` | Narrow the value: the first capture group of the first match, or the whole match when there is no group. No match yields empty. |
| `transform` | A list of named transforms applied in order, at most eight. |
| `layouts` | Go time layouts for `published`, tried before the built-in ones. Not allowed on any other field. |

The value is read, **trimmed**, then matched against `regex`, then passed
through `transform` in order.

#### The fields, and what they become

| Field | Becomes | Notes |
|---|---|---|
| `id` | the result's stable id | Falls back to the infohash, then the details or download address with its query removed, then the title. |
| `title` | the title | **Required.** |
| `infohash` | the infohash | Kept only if it is 40 hex or 32 base32 characters; otherwise taken from the magnet. |
| `magnet` | the magnet URI | Kept only if it really starts `magnet:`. |
| `torrent_url` | the `.torrent` address | Resolved against `base_url`; refused unless `http`/`https`. |
| `size` | the size in bytes | `1.4 GiB`, `700 MB`, `1,024`, `1503238553`. A unit is read as a power of 1024 however it is spelled. A bare number is bytes. |
| `seeders`, `leechers` | the swarm counts | The first run of digits in the value. |
| `category` | tortui's coarse bucket | audio / video / image / text / software / data, or *other*. Anything tortui cannot place is *other* — never dropped. |
| `published` | the date | Parsed with `layouts` first, then a built-in list. Unparseable leaves it blank rather than guessing. |
| `uploader` | the uploader's name | |
| `source_url` | the human-viewable details page | Resolved against `base_url`; refused unless `http`/`https`, so a `javascript:` link on a hostile page never reaches the "open in browser" key. |

Any other key under `fields` is a validation error: a silently ignored field
looks exactly like a selector that has rotted, and the whole point of the file
is that you can tell those apart.

#### Transforms

`trim` · `collapse_whitespace` · `lowercase` · `uppercase` · `digits` ·
`urldecode`

Each is applied once, in the order you list them. `digits` is the one you will
reach for most: it turns `1,204 seeders` into `1204`.

#### Trust

```yaml
trust:
  selector: .badge
  attr: data-trust
  values:
    gold: vip
    silver: trusted
    checked: verified
    plain: none
```

`selector`, `attr`, `regex` and `transform` work exactly as in a field; `values`
maps what you read onto one of `unknown`, `none`, `verified`, `trusted`, `vip`.
Matching ignores case and surrounding space. A value the map does not list, and
a selector that matches nothing, both mean **unknown** — "this site said
nothing", which is different information from `none` ("this site tracks badges
and this uploader has none") and sorts differently.

Trust is a badge in the results table. It never gates anything.

---

## JSON mode

Set `mode: json` and every `selector` becomes a path expression instead of a CSS
selector:

* `.`-separated keys: `page.items`, `links.magnet`
* an all-digit segment indexes an array: `items.0.name`
* `\.` is a literal dot inside a key, `\\` a literal backslash
* an empty selector means the row itself

`rows` must lead to an array. A path that leads nowhere is an empty value, the
same as a CSS selector that matches nothing; a path that leads to something
which is not an array is an error, because the definition is describing the
response wrongly rather than the response being empty. `attr` is meaningless
here and is refused.

Numbers keep their exact text, so a large id does not lose precision and a
seeder count does not come back as `1.204e+03`.

---

## Validation

`tortui` validates a definition before it will use it, and says which key is
wrong:

```
definition search.fields.title.selector: selector "td.name a[": not a valid CSS selector (expected identifier, found EOF instead)
definition search.fields.seeders.transform: "shout" is not a transform this schema defines (they are collapse_whitespace, digits, lowercase, trim, uppercase, urldecode)
definition latest.rows: no rows selector is defined for this block
definition trust.values.gold: "platinum" is not a trust level (they are unknown, none, verified, trusted, vip)
```

Decoding is strict: an unknown key, a duplicate key and a value of the wrong
type are all errors. A YAML syntax error is reported by line and column and
**not** by content — if you had written a token into a parameter value, the
error message must not be the thing that copies it into your log file.

### Limits

| Limit | Value | Why |
|---|---|---|
| Rows per response | 1000 | A selector that matches a million rows is one page away. |
| HTML nesting depth | 512 | Parsing a deeply nested document costs time quadratic in its depth, and the page is not yours. |
| Transforms per field | 8 | A chain is a list applied once; the bound keeps a silly one from multiplying the cost of every row. |
| Response size | 8 MB | Applied by tortui's shared HTTP client, along with the timeouts and the spacing between requests to the same host. |

Regular expressions are Go's RE2: no backreferences, no lookaround, and no way
to write one that backtracks catastrophically.

---

## A complete worked example

This is the definition this repository's own tests run against, for an invented
site on a reserved example domain. The pages it reads are in
`testdata/scraper/search.html` and `testdata/scraper/latest.html`, and the file
itself is `testdata/scraper/fixture-archive.yml`.

The site lists results in a table:

```html
<table class="results">
  <tr class="head"><th>Name</th>…</tr>
  <tr class="row">
    <td class="name"><a href="/item/1001" data-item="1001">Invented Reference Corpus 2026</a></td>
    <td class="size">1.4 GiB</td>
    <td class="seeders">1,204</td>
    <td class="leechers">37</td>
    <td class="kind">Data</td>
    <td class="added" datetime="2026-03-04 11:20:00">4 March 2026</td>
    <td class="by">avery <span class="badge" data-trust="gold">VIP</span></td>
    <td class="links">
      <a class="magnet" data-infohash="0123…4567" href="magnet:?xt=urn:btih:0123…4567">magnet</a>
      <a class="torrent" href="/dl/1001.torrent">torrent</a>
    </td>
  </tr>
  <tr class="promo"><td colspan="8">An advertisement between results.</td></tr>
</table>
```

…and its recent-additions page as a feed, with the same cell classes but the
name in a heading:

```html
<ul class="feed">
  <li class="entry">
    <h3 class="headline"><a href="/item/2001" data-item="2001">Invented Weather Station Dump, March</a></h3>
    <span class="size">512 MiB</span>
    <span class="seeders">88</span>
    <span class="kind">Data</span>
    <span class="added" datetime="2026-03-09 08:00:00">9 March 2026</span>
    <span class="by">avery <span class="badge" data-trust="gold">VIP</span></span>
    <a class="magnet" href="magnet:?xt=urn:btih:1111…1111">magnet</a>
  </li>
</ul>
```

The definition:

```yaml
id: fixture-archive
name: Fixture Archive
base_url: https://archive.example.org
mode: html
requires_auth: false

# One result row. The selector deliberately matches the header and
# promotional rows too: a row with no title is skipped, so there is no need
# for a selector that excludes each of them by hand.
rows: table.results tr

fields:
  id:
    selector: .name a
    attr: data-item

  title:
    selector: .name a

  source_url:
    selector: .name a
    attr: href

  magnet:
    selector: a.magnet
    attr: href

  torrent_url:
    selector: a.torrent
    attr: href

  infohash:
    selector: a[data-infohash]
    attr: data-infohash

  size:
    selector: .size

  seeders:
    selector: .seeders
    transform:
      - digits

  leechers:
    selector: .leechers
    transform:
      - digits

  category:
    selector: .kind

  published:
    selector: .added
    attr: datetime
    layouts:
      - "2006-01-02 15:04:05"
      - "2006-01-02"

  uploader:
    selector: .by
    regex: "^([A-Za-z0-9_.-]+)"

trust:
  selector: .badge
  attr: data-trust
  values:
    gold: vip
    silver: trusted
    checked: verified
    plain: none

search:
  path: /search
  params:
    q: "{{query}}"
    from: "{{offset}}"
    n: "{{limit}}"

# The recent-additions page renders a feed and puts the name in a heading, so
# this block overrides the row selector and the three fields that read the
# name. Everything else — size, seeders, leechers, category, published,
# uploader, magnet, infohash, torrent_url and the whole trust block — is
# inherited from above.
latest:
  path: /latest
  rows: ul.feed li.entry
  fields:
    id:
      selector: .headline a
      attr: data-item
    title:
      selector: .headline a
    source_url:
      selector: .headline a
      attr: href
```

Searching for `corpus` sends `GET https://archive.example.org/search?q=corpus`
(`from` and `n` are left off, because the query asked for no offset and no
limit) and produces, from the row above:

| Field | Value |
|---|---|
| id | `1001` |
| title | `Invented Reference Corpus 2026` |
| infohash | `0123…4567` |
| magnet | `magnet:?xt=urn:btih:0123…4567` |
| torrent_url | `https://archive.example.org/dl/1001.torrent` |
| size | 1 503 238 553 bytes |
| seeders / leechers | 1204 / 37 |
| category | data |
| published | 2026-03-04 11:20:00 |
| uploader | `avery` |
| trust | VIP |
| source_url | `https://archive.example.org/item/1001` |

The header row and the promotional row produce nothing, because neither has a
title.

### The same site's JSON endpoint

`testdata/scraper/fixture-api.yml`, against `testdata/scraper/api.json`:

```yaml
id: fixture-api
name: Fixture Archive API
base_url: https://api.example.org
mode: json

rows: page.items

fields:
  id:
    selector: id
  title:
    selector: name
  size:
    selector: bytes
  seeders:
    selector: swarm.seed
  leechers:
    selector: swarm.leech
  category:
    selector: kind
  published:
    selector: added
  uploader:
    selector: by
  magnet:
    selector: links.magnet
  torrent_url:
    selector: links.torrent
  source_url:
    selector: page

trust:
  selector: flag
  values:
    gold: vip
    plain: none

search:
  path: /api/search
  params:
    q: "{{query}}"
    offset: "{{offset}}"
```

---

## Where definitions live, and when they are read

Put each definition in its own file in the `definitions/` directory inside your
config directory — `~/.config/tortui/definitions/` on macOS and Linux (or
`$XDG_CONFIG_HOME/tortui/definitions/` when you have set that variable),
`%AppData%\tortui\definitions\` on Windows. The directory does not have to
exist: nothing creates it for you, and starting with no definitions at all is an
ordinary state rather than an error.

What is read, exactly:

* **Files ending in `.yml`, in that one directory.** The extension is matched
  without regard to case, so `archive.YML` is read on every platform. A `.yaml`
  file is not read; nor is anything in a sub-directory.
* **One file per source.** Files are read in filename order and the resulting
  set is ordered by `id`.
* **A file at most 1 MiB.** A larger one is skipped without being parsed.
* **Each `id` once.** If two files declare the same `id`, the first in filename
  order is used and the second is skipped, so which one wins does not depend on
  the order your filesystem happens to list them in.

**A broken definition never stops tortui starting.** Every file is read
independently, and one that will not parse or will not validate is skipped and
written to the log file with its name and the reason — the rest of the directory
loads as usual. Fix the file and reload; nothing else was affected. The whole
read only fails if the directory itself cannot be listed (a permissions
problem), and in that case tortui keeps the definitions it had already loaded.

**Reloading** re-reads the whole directory and swaps the set in one step:
anything reading the set at that moment sees either all of the old definitions
or all of the new ones, and never a mixture of the two. There is no
partially-applied state, and no restart is needed after editing a file. (A file
that is still being written when the reload reads it is simply skipped like any
other unparseable one; reload again once your editor has saved it.)

## What the framework deliberately does not do

* **No credential discovery, session harvesting, captcha solving or paywall
  workaround**, in the schema or anywhere else. The only authentication there is
  is a credential you took from your own account and put in your own config.
* **No category push-down.** There is no way to say which of a site's own
  category values correspond to tortui's buckets, so a category filter is not
  sent to a scraped source and is not applied locally either. The `category`
  field still classifies each result for the table.
* **No details-page fetch.** A result that carries only a details link cannot
  have a magnet fetched out of that page yet; a definition needs to read the
  magnet, the torrent link or the infohash off the listing itself.
* **No repeated or scheduled fetching.** A definition cannot ask tortui to poll.
  Refreshing is something the user does, and there is a minimum interval between
  requests to the same host.
