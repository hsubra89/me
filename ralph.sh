#!/usr/bin/env bash
# ralph.sh
# Usage: ./ralph.sh <iterations|review>

set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <iterations|review>"
  exit 1
fi

command -v codex >/dev/null || { echo "codex not installed or not on PATH"; exit 1; }
trap 'echo "interrupted"; exit 130' INT
rm -rf .ralph
mkdir -p .ralph

command_arg="$1"
iterations="$command_arg"
base_branch="${RALPH_BASE_BRANCH:-$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null | sed 's#^origin/##')}"
base_branch="${base_branch:-main}"
last_review_iteration=0

has_review_notes() {
  grep -qiE '^#[[:space:]]*review notes[[:space:]]*$' issues.md
}

run_review_pass() {
  local label="$1"
  local iteration="$2"
  local safe_label="${label//[^A-Za-z0-9_.-]/-}"
  local review_file=".ralph/review-${safe_label}.txt"
  local review_log=".ralph/review-${safe_label}.log"
  local judge_file=".ralph/review-${safe_label}-judge.log"
  local review_prompt
  local judge_prompt
  local review_status
  local judge_status

  echo "=== review pass: $label (base: $base_branch) ==="

  printf -v review_prompt '%s\n' \
    "Review the current branch against the '$base_branch' branch, taking into account the PRD in prd.md and the issues done in issues.md." \
    "Current progress is in progress.txt." \
    "" \
    "Focus on correctness bugs, behavioral regressions, missing tests, and architectural risks in the implementation so far that won't be addressed by future issues in issues.md." \
    "Return actionable findings with file references when possible."

  set +e
  codex exec review \
    -m gpt-5.5 \
    -c model_reasoning_effort=xhigh \
    -o "$review_file" \
    "$review_prompt" 2>&1 | tee "$review_log"
  review_status=${PIPESTATUS[0]}
  set -e

  if [ "$review_status" -ne 0 ]; then
    echo "review pass $label failed with exit $review_status, continuing"
    return 0
  fi

  printf -v judge_prompt '%s\n' \
    "Read the review output from $review_file, judge which findings are valid and important, and update issues.md." \
    "If the review output mentions a note that won't be valid once a future issue in issues.md is implemented, remove the note from the review notes section." \
    "" \
    "Write or replace a top-of-file section titled exactly:" \
    "# Review notes" \
    "" \
    "The Review notes section should contain the highest-priority actionable notes future iterations should address before ordinary issues. Deduplicate findings, discard low-confidence or non-actionable feedback, and keep the notes concise. If no actionable notes remain or the existing notes are no longer valid, remove the section entirely." \
    "" \
    "Do not modify files other than issues.md. Make a git commit when done."

  echo "=== judging review pass: $label ==="
  set +e
  codex exec --dangerously-bypass-approvals-and-sandbox \
    -m gpt-5.5 \
    -c model_reasoning_effort=xhigh \
    --color never "$judge_prompt" 2>&1 | tee "$judge_file"
  judge_status=${PIPESTATUS[0]}
  set -e

  if [ "$judge_status" -ne 0 ]; then
    echo "review judge $label failed with exit $judge_status, continuing"
    return 0
  fi

  last_review_iteration="$iteration"
}

run_review_notes_fixup() {
  local label="$1"
  local safe_label="${label//[^A-Za-z0-9_.-]/-}"
  local fixup_file=".ralph/review-notes-${safe_label}.log"
  local fixup_prompt
  local fixup_status

  echo "=== review notes fixup: $label ==="

  fixup_prompt="$(cat <<'EOF'
Read prd.md, issues.md, progress.txt, and especially the top `# Review notes` section in issues.md.

Address all of the highest-priority actionable review notes before ordinary issues.
Check any relevant feedback loops, such as types and tests.
Append your progress to progress.txt.
Update issues.md when the review note is resolved, and remove the `# Review notes` section entirely when there are no remaining actionable review notes.

Make a git commit for the fixup.
ONLY WORK ON REVIEW NOTES IN THIS PASS.
EOF
)"

  set +e
  codex exec --dangerously-bypass-approvals-and-sandbox \
    -m gpt-5.5 \
    -c model_reasoning_effort=xhigh \
    --color never "$fixup_prompt" 2>&1 | tee "$fixup_file"
  fixup_status=${PIPESTATUS[0]}
  set -e

  if [ "$fixup_status" -ne 0 ]; then
    echo "review notes fixup $label failed with exit $fixup_status, continuing"
  fi
}

push_current_branch() {
  echo "=== pushing current branch ==="
  git push
}

if [ "$command_arg" = "review" ]; then
  run_review_pass "manual" 0
  exit 0
fi

# For each iteration, run Codex with the following prompt.
for ((i=1; i<=iterations; i++)); do
  echo "=== iteration $i / $iterations ==="
  last_file=".ralph/iter-$i.last.txt"
  log_file=".ralph/iter-$i.log"

  if has_review_notes; then
    run_review_notes_fixup "iter-$i"
  fi

  prompt="$(cat <<'EOF'
Read prd.md, issues.md, and progress.txt.
1. Pick a task to work on next from issues.md.
This should be the one YOU decide has the highest priority, not necessarily the first in the list.
If the task involves implementing a feature, use $tdd if possible.
If you realise that more tasks are needed to meet the objectives of the PRD, add them to issues.md.
2. Check any feedback loops, such as types and tests.
3. Append your progress to the progress.txt file.
4. Update status of tasks in issues.md.
5. Make a git commit of that feature.
ONLY WORK ON A SINGLE TASK / FEATURE.
If, while implementing the feature, you notice that all work
is complete, output <promise>COMPLETE</promise>.
EOF
)"

  set +e
  codex exec --dangerously-bypass-approvals-and-sandbox \
    -m gpt-5.5 \
    -c model_reasoning_effort=xhigh \
    --color never -o "$last_file" "$prompt" 2>&1 | tee "$log_file"
  status=${PIPESTATUS[0]}
  set -e

  if [ "$status" -ne 0 ]; then
    echo "iteration $i failed with exit $status, continuing"
    push_current_branch
    continue
  fi

  if [ -f "$last_file" ] && grep -q "<promise>COMPLETE</promise>" "$last_file"; then
    echo "PRD complete, exiting."
    if [ "$i" -ne 1 ]; then
      run_review_pass "complete-iter-$i" "$i"
    fi
    if has_review_notes; then
      run_review_notes_fixup "complete-iter-$i"
    fi
    push_current_branch
    exit 0
  fi

  if [ $((i % 3)) -eq 0 ]; then
    run_review_pass "iter-$i" "$i"
  fi

  push_current_branch
done

if [ "$last_review_iteration" -ne "$iterations" ]; then
  run_review_pass "final-iter-$iterations" "$iterations"
fi

if has_review_notes; then
  run_review_notes_fixup "final-iter-$iterations"
fi

push_current_branch
