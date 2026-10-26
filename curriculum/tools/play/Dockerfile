# A small stand-in for the session image, used only by
# curriculum/tools/play/play.py to replay lesson demos against a real
# Emacs 31.1 before the real session image and `playctl curriculum play`
# exist. It is NOT the session image (images/session, emacs.md §1).
#
#   docker build -t pg-curriculum-play curriculum/tools/play
#
FROM silex/emacs:31.1-ci

RUN apt-get update \
 && apt-get install -y --no-install-recommends gcc g++ libc6-dev git ca-certificates ripgrep \
 && rm -rf /var/lib/apt/lists/*

COPY install.el /opt/play/install.el
RUN mkdir -p /opt/play/elpa /opt/play/tree-sitter \
 && HOME=/opt/play/home emacs --batch -l /opt/play/install.el \
 && chmod -R a+rX /opt/play
