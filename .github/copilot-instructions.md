# Copilot Instructions

## Interaction Guidelines

When mentioned in comments (e.g., "@copilot what do you think?"), provide feedback, opinions, or code review suggestions **without creating code changes or new commits** unless explicitly requested. This project primarily uses Copilot for advisory purposes and code reviews, not automatic code generation.

Only create code changes when:
- Explicitly asked to implement something (e.g., "implement X", "add Y", "fix Z")
- Responding to actionable feedback that requires code modifications

When providing advisory responses, reply directly to the comment without using `report_progress` or creating commits.

## Code Review Guidelines

### Previous Review Comments

Before suggesting changes, read the comments on previous reviews in this PR. If a reviewer has already justified a decision or explained why a particular approach was chosen, do not repeat the same suggestion. Only raise an issue again if you fundamentally disagree with the reviewer's justification and can provide a compelling counter-argument.

### Testing

This project currently has no test infrastructure. Do not suggest adding tests or flag missing test coverage in code reviews. Test coverage improvements are tracked separately and will be addressed in a dedicated effort.
