# Contributing

## Branches

Format: `<subproject>/<short-description>`

Use the touched subproject or `repo` for repo-wide changes.

### Example

```
editor/inline-error-markers
```

## Commits

Format: `<type>: <summary>\n- <what changed>\n- <what changed>`

Types: `feat` `fix` `docs` `refactor` `test` `ci` `chore`

If an AI agent contributed, add a `Co-Authored-By:` trailer.

### Example

```
feat(editor): add inline error markers
- Parse compiler diagnostics into editor ranges
- Render diagnostics beside affected lines

Co-Authored-By: Claude Fable 5 (1M context) <noreply@anthropic.com>
```

## Pull Requests

Title format: `<Subproject>: <Summary>`

Use a clear, concise Title Case summary.

### Example

```
Editor: Inline Error Markers
```
