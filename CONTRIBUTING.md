# Contributing

## Development

#### Workspace setup

```bash
# create a folder
mkdir ottrec
cd ottrec

# clone the repositories
git clone https://github.com/pgaskin/ottrec ottrec
git clone https://github.com/pgaskin/ottrec-website website
git clone https://github.com/pgaskin/ottrec-data data --filter=blob:none
git clone https://github.com/pgaskin/ottrec-misc misc
git clone https://github.com/pgaskin/ottrec-infra infra
git -C data worktree add ../cache cache

# set up the go workspace
go work init
go work use ./ottrec
go work use ./website
go work use ./misc

# set up tofu
env -C infra/terraform tofu init

# optional: define some useful aliases
alias ottrec-root='dirname "$(go env GOWORK)"'
alias croot='cd "$(ottrec-root)"'
```

#### VSCode extensions

- [`golang.go`](https://marketplace.visualstudio.com/items?itemName=golang.Go)
- [`a-h.templ`](https://marketplace.visualstudio.com/items?itemName=a-h.templ)
- [`pbkit.vscode-pbkit`](https://marketplace.visualstudio.com/items?itemName=pbkit.vscode-pbkit)
- [`aaron-bond.better-comments`](https://marketplace.visualstudio.com/items?itemName=aaron-bond.better-comments)
- [`dnut.rewrap-revived`](https://marketplace.visualstudio.com/items?itemName=dnut.rewrap-revived) - for rewrapping comments (Alt+A)
- [`yo1dog.cursor-align`](https://marketplace.visualstudio.com/items?itemName=yo1dog.cursor-align)
- [`opentofu.vscode-opentofu`](https://marketplace.visualstudio.com/items?itemName=OpenTofu.vscode-opentofu)
- [`hangxingliu.vscode-systemd-support`](https://marketplace.visualstudio.com/items?itemName=hangxingliu.vscode-systemd-support)
- [`PKief.material-icon-theme`](https://marketplace.visualstudio.com/items?itemName=PKief.material-icon-theme)

#### VSCode settings

```jsonc
{
    "[go]": {
        "editor.codeActionsOnSave": {
            "source.organizeImports": "explicit"
        },
        "editor.rulers": [
            80
        ],
        "editor.formatOnSave": true,
    },
    "[templ]": {
        "editor.defaultFormatter": "a-h.templ",
        "editor.formatOnSave": true,
        "editor.wordWrap": "on",
    },
    "[opentofu]": {
        "editor.formatOnSave": true
    },
    "workbench.iconTheme": "material-icon-theme",
}
```

### Scraper

#### Running the unit tests

```bash
go test -v ./ottrec/...
```

#### Running the scraper locally using cached data

```bash
# optional: reset the cache and data to the latest upstream version
git -C cache clean -fdx
git -C data clean -fdx
git -C cache reset --hard HEAD
git -C data reset --hard HEAD
git -C cache pull
git -C data pull

# run the scraper
go run ./ottrec/scraper -cache ./cache -geocodio -scrape -export.pretty -export.proto ./data/data.proto -export.pb ./data/data.pb -export.textpb ./data/data.textpb -export.json ./data/data.json

# inspect the changes
git -C data diff
```

#### Updating the schema

Also run this when updating the protobuf module.

- Do not make backwards-incompatible protobuf changes.
- Avoid renaming fields unless absolutely necessary (this will break JSON users).
- Avoid adding fields which do not mirror the inherent structure of the website and could be consistently computed from the existing fields.
- Underscored fields can be used for computed fields where how they are parsed may change in the future, but the field itself is an inherent property (e.g., schedule date ranges from the caption).
- Do not change the semantic meaning of existing fields.
- Do not remove fields entirely; deprecate them, but continue to set them.
- Keep fields in sync with the website ottrecidx package.

If backwards-incompatible changes are ever necessary, create a new v2 subdir with the new schema, put stuff in there, create a new v2 api using the v2 schema, and create a new v2 branch in the data repo.

```bash
buf lint ./ottrec/schema/schema.proto # ignore the warnings about underscored field names, v1 dir, and the weekday enum
buf breaking --against ./data/data.proto ./ottrec/schema/schema.proto
go generate ./ottrec/schema
```

### Website

#### Some notes

Be careful with the packages in pkg/* (data export, simplified schema, query language, indexer, heuristics, storage). Never use Claude on them, and don't touch them carelessly or do unnecessary refactoring either. They were very carefully designed to handle the schedule data correctly and efficiently, and to preserve backwards/forwards compatibility. Certain packages (e.g., ottrecql and ottrecidx) are also somewhat fragile and easy to break in subtle ways.

However, as a result of this careful backend work, Claude tends to do a good job with the frontend templates/scripts/styles as long as you have a clear vision.

CSS should be written for compatibility, but you can safely use most modern features since it's transpiled at startup using LightningCSS.

Scripts should be written in TypeScript. Prefer to write modern JS instead of importing libraries. All pages should also fall back gracefully without scripting. If you must import a dependency, choose a well-designed modern one without a crazy tree of dependencies.

Fonts are subsetted from Google Fonts and committed. See fonts.go.

You may wonder why I don't use a "proper" frontend framework or at least a separate build step, and it's because:

- I want server-rendered progressively-enhanced pages because SPAs suck (and SEO is also easier this way).
- I hate "modern" frontend framework-driven development.
- I can use templ for JSX-like syntax in Go (which also ties in really well with my custom indexer).
- Go already has a very nice and mature JS bundler/transpiler, esbuild.
- I ported LightningCSS (a nice CSS transpiler written in Rust) to Go using wasm2go.
- NPM is a security nightmare. Vendoring the deps and building them with esbuild sidesteps this entirely.
- Since LightningCSS and esbuild are very fast, I can build stuff at startup.
- As a result, I can build and run with just the Go toolchain, and also get a tight feedback loop during development.

And if you're wondering why I rolled my own indexer instead of using a separate database:

- Again, I control the entire stack, so I can have nice things.
- The custom indexer also allows heuristics to be computed efficiently and without needing to keep logic in sync with an external datastore.
- Since everything is in Go, I can do fancy stuff with iterators so the API feels natural.
- I can do interning to amortize the memory cost of loading many versions at the same time.
- Almost every page render needs to read almost an entire version of the data and filter it in some way. This design lets me do filtering with zero copies and very little overhead.
- Without this, a lot of cycles would be wasted normalizing and denormalizing the data in/out of the heirarchical form needed for rendering.
- It was very fun to design and write.

When doing URL parameters, handle them such that errors are obvious, yet still have them tolerant to unknown params, and also design them carefully for forwards/backwards compatibility.

All pages should have sensible caching, making use of ETags where possible.

Pages should also have canonical URLs set, pointing at the main page of that category to avoid being penalized by Google for the many generated pages.

#### Running it locally with automatic restart

```bash
export DEBUG_POSTCSS_NOOP=1 # optional: don't process stylesheets with postcss
env -C website watchexec --clear --debounce 1s -f '*.templ' --watch ./templates 'go generate ./templates'
env -C website watchexec --clear --debounce 1s -i '*.templ' --restart 'go run ./cmd/ottrec-data' # http://data.ottrec.localhost:8082/
env -C website watchexec --clear --debounce 1s -i '*.templ' --restart 'go run ./cmd/ottrec-website' # http://ottrec.localhost:8083/
```

alternatively:

```bash
env -C website go run ./cmd/ottrec-data
DEBUG_POSTCSS_NOOP=1 env -C website templ generate --watch --watch-pattern='[.](go|templ|css|js)$' --proxy="http://127.0.0.1:8183" --proxyport="8083" --proxybind="0.0.0.0" --cmd="go run ./cmd/ottrec-website --addr 127.0.0.1:8183"
```

#### Updating fonts and JS libs

```bash
go generate ./website/static
```

#### Running ottrecidx sanity checks

```bash
go run ./website/pkg/ottrecidx/profile.go -check
```

#### Profiling ottrecidx

```bash
go run ./pkg/ottrecidx/profile.go -cpuprofile /tmp/cpu.pprof -memprofile /tmp/mem.pprof
go tool pprof -http :6060 /tmp/cpu.pprof
go tool pprof -http :6061 /tmp/mem.pprof
```

### Infra

```bash
cd infra/terraform
git pull
tofu apply
git commit -a
```

```bash
cd infra/ansible
ansible-playbook -i inventory.yml playbook.yml # everything
ansible-playbook -i inventory.yml playbook.yml -t ottrec # only ottrec
```
