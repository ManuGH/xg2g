# Issues & Planning

This directory contains detailed planning documents for larger refactoring tasks, feature development, and architectural improvements.

## Purpose

Issues in this directory provide:
- **Detailed problem statements** - Why is this work needed?
- **Proposed solutions** - What approach will be taken?
- **Task breakdowns** - Step-by-step implementation plan
- **Test checklists** - Ensure quality throughout
- **Risk assessments** - Identify and mitigate risks
- **Success metrics** - Measure improvement

## When to Create an Issue Document

Create an issue document when:
- Task requires **> 2 hours** of work
- Multiple files/modules are affected
- Breaking changes are involved
- Refactoring impacts existing functionality
- External stakeholders need visibility
- Implementation requires coordination

## Issue Template Structure

Each issue should include:

1. **Metadata**
   - Status (Planned/In Progress/Completed/Blocked)
   - Priority (Low/Medium/High/Critical)
   - Effort estimate
   - Assignee

2. **Problem Statement**
   - What is the current issue?
   - Why does it need to be solved?
   - What is the impact?

3. **Proposed Solution**
   - High-level approach
   - Alternative solutions considered
   - Why this solution was chosen

4. **Milestones**
   - Break work into manageable chunks
   - Each milestone with tasks and checklist
   - Deliverables clearly defined

5. **Test Strategy**
   - Unit tests
   - Integration tests
   - Performance tests
   - Acceptance criteria

6. **Risk Assessment**
   - Potential issues
   - Mitigation strategies
   - Rollback plan

7. **Success Metrics**
   - Measurable improvements
   - Before/after comparisons

## Current Issues

### Active
- None

### Planned
- [API_REFACTORING.md](./API_REFACTORING.md) - Split `internal/api/http.go` into focused modules

### Completed
- None yet

## Issue Lifecycle

```
Planned → In Progress → Code Review → Testing → Completed
                  ↓
              Blocked (if issues arise)
```

## Best Practices

### Planning
- Start with problem statement, not solution
- Consider multiple approaches
- Document trade-offs
- Get feedback early

### Implementation
- Work in small, atomic commits
- Complete one milestone before starting next
- Run tests after each milestone
- Document as you go

### Testing
- Write tests before implementation (TDD)
- Maintain or improve coverage
- Include performance tests
- Add integration tests for critical paths

### Documentation
- Update issue status regularly
- Document blockers immediately
- Note deviations from plan
- Record actual completion time

## Contributing

When working on an issue:

1. Update status to "In Progress"
2. Create feature branch: `feature/issue-name` or `refactor/issue-name`
3. Follow milestone order
4. Commit after each milestone
5. Update issue with progress
6. Submit PR when complete
7. Link PR to issue
8. Update status to "Completed" after merge

## Questions?

For questions about issues or planning:
- Check existing issue documents for examples
- Review [CONTRIBUTING.md](../../CONTRIBUTING.md) (if exists)
- Ask in team chat/discussion
- Open discussion issue on GitHub

---

**Note:** This directory complements GitHub Issues but provides more detailed planning for complex technical work.
