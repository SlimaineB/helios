FROM python:3.11-alpine
RUN apk add --no-cache git
WORKDIR /srv
# Build a dumb-HTTP git server from the demo playbook, with a short
# main-branch history and semver tags so tag/semver targetRevision works.
# Each release bumps the `release:` var rendered by webapp.conf.j2 — the
# highest matching tag is what the agent checks out. "latest" is deliberately
# non-semver (must be ignored by ranges).
COPY examples/playbooks/demo /tmp/demo-src
COPY examples/playbooks/archive /tmp/archive-src
COPY examples/playbooks/cmd /tmp/cmd-src
RUN set -e \
    && G() { git -C /tmp/demo-src "$@"; } \
    && C() { G -c user.email=boot@ansiblehub -c user.name=boot commit -qam "$1"; } \
    && B() { sed -i "s/^        release: .*/        release: $1/" /tmp/demo-src/site.yml; } \
    && G init -b main -q \
    && G add -A \
    && B v0.9.0 && C v0.9.0 && G tag v0.9.0 \
    && B v1.0.0 && C v1.0.0 && G tag v1.0.0 \
    && B v1.1.0 && C v1.1.0 && G tag v1.1.0 \
    && B v1.2.0 && C v1.2.0 && G tag v1.2.0 && G tag latest \
    && git clone -q --bare /tmp/demo-src /srv/demo.git \
    && git --git-dir=/srv/demo.git update-server-info \
    && touch /srv/demo.git/git-daemon-export-ok \
    && rm -rf /tmp/demo-src \
    && mkdir -p /srv/static \
    && cp /tmp/archive-src/asset/hello.tar.gz /srv/static/hello.tar.gz \
    && rm -rf /tmp/archive-src/asset \
    && A() { d="$1"; n="$2"; git -C "$d" init -b main -q \
        && git -C "$d" -c user.email=boot@ansiblehub -c user.name=boot add -A \
        && git -C "$d" -c user.email=boot@ansiblehub -c user.name=boot commit -qm init \
        && git clone -q --bare "$d" "/srv/$n.git" \
        && git --git-dir="/srv/$n.git" update-server-info \
        && touch "/srv/$n.git/git-daemon-export-ok" \
        && rm -rf "$d"; } \
    && A /tmp/archive-src archive \
    && A /tmp/cmd-src cmd
EXPOSE 8000
CMD ["python3", "-m", "http.server", "8000", "--directory", "/srv"]