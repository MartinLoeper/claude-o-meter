# Finish Feature

**Arguments:** $ARGUMENTS

If arguments are provided, use them as the merge comment. Otherwise, proceed without a merge comment.

Run this from the feature worktree directory.

1. **Ask about version bump**:
   - Show current VERSION file contents
   - Ask: "Do you want to bump the VERSION file?"
   - If yes, ask what the new version should be and update the file

2. **Commit any uncommitted changes**:
   - Run `git status` to check for changes
   - If there are changes, commit them with an appropriate message

3. **Push the branch**:
   ```bash
   git push
   ```

4. **Update the PR description** with a summary of what was accomplished (update the Tasks section, add completion notes)

5. **Mark PR as ready for review**:
   ```bash
   gh pr ready
   ```

6. **Ask the user to merge** the PR:
   - Show the PR URL
   - Ask: "Ready to merge this PR into main?"
   - If yes, run: `gh pr merge --merge --delete-branch` (add `-b "<merge comment>"` if a merge comment was provided via arguments)

7. **Output the cd command** to return to the main repository:
   ```
   cd ~/repos/MartinLoeper/<main-repo-path>
   ```
   (derive the main repo path by removing `-worktrees/<branch>` from current directory)
