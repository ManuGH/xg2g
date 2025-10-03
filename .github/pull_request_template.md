# Pull Request

## Description

> **Note**: This project follows an English-only policy. Please write your pull request in English to ensure it can be understood by all contributors.

Brief description of the changes in this PR.

## Type of Change

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation update
- [ ] Code refactoring (no functional changes)
- [ ] Performance improvement
- [ ] Test coverage improvement

## Changes Made

- List the main changes
- Be specific about what was modified
- Include any new dependencies or configuration

## Testing

- [ ] Unit tests pass (`go test ./... -race`)
- [ ] Linter passes (`make lint`)
- [ ] Build succeeds (`go build ./cmd/daemon`)
- [ ] Manual testing performed (describe below)

### Manual Testing

Describe how you tested these changes:

```bash
# Example testing commands
XG2G_DATA=./data XG2G_OWI_BASE=http://test.local ./daemon
curl http://localhost:8080/api/status
```

## Configuration Changes

- [ ] No configuration changes
- [ ] New environment variables added (document below)
- [ ] Existing configuration behavior changed (document below)

### New/Changed Configuration

If applicable, list new or changed environment variables:

- `XG2G_NEW_SETTING`: Description of what it does and default value

## Breaking Changes

- [ ] No breaking changes
- [ ] Breaking changes (describe impact and migration path below)

### Migration Path

If there are breaking changes, describe how users should migrate:

1. Step one
2. Step two

## Related Issues

Closes #(issue number)
Relates to #(issue number)

## Checklist

- [ ] My code follows the project's coding standards
- [ ] I have performed a self-review of my code
- [ ] I have added tests that prove my fix/feature works
- [ ] New and existing tests pass locally
- [ ] I have updated documentation as needed
- [ ] My changes generate no new warnings or errors
