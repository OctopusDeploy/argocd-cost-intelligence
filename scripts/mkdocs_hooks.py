"""MkDocs hooks for the Read the Docs site.

The Markdown under docs/ is written to be read on GitHub as well, so it links to the README and to
files outside docs/ (the chart, CONTRIBUTING.md, ...). These hooks keep it that way:

- the README becomes the site's home page, and ROADMAP.md and SECURITY.md its Roadmap and Security
  pages, so none of them is kept twice;
- a link to one of them goes to its page, and a link to any other file outside docs/ goes to that
  file on GitHub, at the commit being built when Read the Docs builds it. A link to a file that
  doesn't exist is a warning, so a strict build fails on it.
"""

import logging
import os
import re

from mkdocs.structure.files import File

REPO_URL = "https://github.com/OctopusDeploy/argocd-cost-intelligence"

# An inline Markdown link or image target, with an optional title: ](target) or ](target "title").
LINK = re.compile(r'\]\(([^)\s]+)(\s+"[^"]*")?\)')
SCHEME = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*:")
FENCE = re.compile(r"^\s*(```|~~~)")

log = logging.getLogger("mkdocs.hooks.links")


def _repo_root(config):
    return os.path.dirname(os.path.abspath(config.config_file_path))


# Files outside docs/ that the site serves as pages: repository file -> page.
PAGES = {"README.md": "index.md", "ROADMAP.md": "roadmap.md", "SECURITY.md": "security.md"}


def on_files(files, config):
    for source, page in PAGES.items():
        with open(os.path.join(_repo_root(config), source), encoding="utf-8") as f:
            generated = File.generated(config, page, content=f.read())
        # Relative to edit_uri (edit/main/docs/), so the edit button opens the repository file.
        generated.edit_uri = "../" + source
        files.append(generated)
    return files


def on_page_markdown(markdown, page, config, files):
    root = _repo_root(config)
    docs = os.path.abspath(config.docs_dir)
    pages = {os.path.join(root, source): os.path.join(docs, page) for source, page in PAGES.items()}
    ref = os.environ.get("READTHEDOCS_GIT_COMMIT_HASH", "main")

    # Where the Markdown lives in the repository, which its links are relative to, and where the
    # page lives in the site. They differ only for the pages generated from PAGES.
    page_dir = os.path.dirname(os.path.join(docs, page.file.src_uri))
    source_dir = root if page.file.abs_src_path is None else page_dir

    def rewrite(match):
        target = match.group(1)
        if target.startswith(("#", "/")) or SCHEME.match(target):
            return match.group(0)
        path, sep, anchor = target.partition("#")
        path, qsep, query = path.partition("?")
        resolved = os.path.normpath(os.path.join(source_dir, path))
        resolved = pages.get(resolved, resolved)
        if resolved.startswith(docs + os.sep):
            new = os.path.relpath(resolved, page_dir).replace(os.sep, "/")
        else:
            if not os.path.exists(resolved):
                log.warning("Doc file '%s' links to '%s', which does not exist", page.file.src_uri, target)
            kind = "tree" if os.path.isdir(resolved) else "blob"
            new = f"{REPO_URL}/{kind}/{ref}/{os.path.relpath(resolved, root).replace(os.sep, '/')}"
        return f"]({new}{qsep}{query}{sep}{anchor}{match.group(2) or ''})"

    # Leave fenced code blocks alone: a command in one may contain "](".
    lines, in_fence = [], False
    for line in markdown.split("\n"):
        if FENCE.match(line):
            in_fence = not in_fence
        lines.append(line if in_fence or FENCE.match(line) else LINK.sub(rewrite, line))
    return "\n".join(lines)
