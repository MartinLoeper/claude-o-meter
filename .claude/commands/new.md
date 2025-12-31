# Start New Feature

**Arguments:** $ARGUMENTS

If arguments are provided, use them as the feature description. Otherwise, ask the user for a feature description.

Then:

1. **Derive branch name** from the description (lowercase, kebab-case, max 50 chars, e.g., "Add user authentication" → `add-user-authentication`)

2. **Create the branch** from `main`:
   ```bash
   git checkout main && git pull
   git checkout -b <branch-name>
   git push -u origin <branch-name>
   ```

3. **Create a git worktree** at `~/repos/MartinLoeper/<current-repo-name>-worktrees/<branch-name>`:
   ```bash
   git worktree add ~/repos/MartinLoeper/<current-repo-name>-worktrees/<branch-name> <branch-name>
   ```

4. **Create a draft PR** on GitHub with:
   - Title: derived from feature description
   - Body:
     ```markdown
     ## Goal
     <feature description from user>

     ## Tasks
     - [ ] Implementation tasks (fill in as you plan)

     ## Notes
     Worklog and decisions will be tracked here.
     ```
   ```bash
   gh pr create --draft --title "<title>" --body "<body>" --base main
   ```

5. **Output the cd command** for the user to copy:
   ```
   cd ~/repos/MartinLoeper/<current-repo-name>-worktrees/<branch-name>
   ```
