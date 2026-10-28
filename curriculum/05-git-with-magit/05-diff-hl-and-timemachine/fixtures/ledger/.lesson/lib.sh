# Shared by the Part 5 setups (setup.run: sh .lesson/<lesson>.sh). Each
# setup rebuilds the repository from scratch with fixed authors and dates,
# so commit ids are the same on every load, then removes .lesson.
# Buffers of the previous lesson may run git in here meanwhile (diff-hl,
# Magit): removing .git and writing the index are retried.
set -eu
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
retry() {
  i=0
  until "$@"; do
    i=$((i + 1)); [ $i -lt 20 ] || return 1
    sleep 0.2
  done
}
fresh() {
  retry rm -rf .git 2>/dev/null
  git init -q -b main
  git config user.name Learner
  git config user.email learner@playground.invalid
  git config commit.gpgsign false
  echo /.lesson/ >> .git/info/exclude
}
# commit "Name" "YYYY-MM-DD HH:MM" "message": everything in the tree.
commit() {
  slug=$(printf %s "$1" | tr 'A-Z ' 'a-z.')
  retry git add -A
  GIT_AUTHOR_NAME="$1" GIT_AUTHOR_EMAIL="$slug@example.com" \
  GIT_COMMITTER_NAME="$1" GIT_COMMITTER_EMAIL="$slug@example.com" \
  GIT_AUTHOR_DATE="$2:00 +0100" GIT_COMMITTER_DATE="$2:00 +0100" \
    retry git commit -q --no-verify -m "$3"
}
done_setup() { rm -rf .lesson; }
