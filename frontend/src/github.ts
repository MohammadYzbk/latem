// The GitHub surfaces: connecting an account, and picking a repository to open.
//
// Both are modal, because both are things you do occasionally and deliberately
// — unlike the palette, which is a thing you do constantly. They borrow the
// palette's shell so the app has one dialog language rather than two.

import {
  CloneRepository,
  GitHubAccount,
  GitHubAwaitLogin,
  GitHubCancelLogin,
  GitHubDisconnect,
  GitHubRepositories,
  GitHubStartLogin,
  GitHubUseToken,
} from '../wailsjs/go/main/App';
import { BrowserOpenURL, ClipboardSetText } from '../wailsjs/runtime/runtime';
import type { forge, main } from '../wailsjs/go/models';
import { fuzzy, highlight } from './palette';

/** Opens a modal and returns a handle for closing it. */
function modal(label: string): { panel: HTMLDivElement; close: () => void } {
  const overlay = document.createElement('div');
  overlay.className = 'palette-overlay';
  overlay.innerHTML = `<div class="palette sheet" role="dialog" aria-modal="true" aria-label="${label}"></div>`;
  document.body.append(overlay);

  const panel = overlay.querySelector<HTMLDivElement>('.sheet')!;
  const close = () => overlay.remove();

  overlay.addEventListener('mousedown', (event) => {
    if (event.target === overlay) close();
  });
  overlay.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      close();
    }
  });
  return { panel, close };
}

function element<T extends HTMLElement>(html: string): T {
  const template = document.createElement('template');
  template.innerHTML = html.trim();
  return template.content.firstElementChild as T;
}

// --- connecting --------------------------------------------------------------

/**
 * Shows the sign-in sheet.
 *
 * Two routes are offered because neither covers everyone: the browser flow
 * needs this build to carry an OAuth client ID, and a pasted token is the only
 * option for organisations that block OAuth apps outright.
 */
export async function connectGitHub(onChanged: (account: main.GitHubAccount) => void) {
  const { panel, close } = modal('Connect GitHub');

  const render = (account: main.GitHubAccount) => {
    panel.replaceChildren();

    if (account.connected) {
      panel.append(
        element(`
          <div class="sheet-body">
            <h2 class="sheet-title">Connected to GitHub</h2>
            <p class="sheet-text">Signed in as <strong>${escape(account.login)}</strong>${
              account.name ? ` (${escape(account.name)})` : ''
            }.</p>
          </div>
        `),
      );
      const actions = element<HTMLDivElement>('<div class="sheet-actions"></div>');
      const disconnect = button('Disconnect', 'btn', async () => {
        const next = await GitHubDisconnect();
        onChanged(next);
        render(next);
      });
      actions.append(button('Done', 'btn btn-quiet', close), disconnect);
      panel.append(actions);
      return;
    }

    const body = element<HTMLDivElement>(`
      <div class="sheet-body">
        <h2 class="sheet-title">Connect GitHub</h2>
        <p class="sheet-text">Open a repository and edit it as a project.</p>
      </div>
    `);
    panel.append(body);

    if (account.error) body.append(note(account.error, 'bad'));

    if (account.deviceFlow) {
      body.append(
        element(`<p class="sheet-text">Sign in through your browser — no password is typed here.</p>`),
      );
      const start = element<HTMLDivElement>('<div class="sheet-actions"></div>');
      start.append(button('Sign in with GitHub', 'btn', () => void browserFlow(panel, onChanged, close, render)));
      body.append(start);
    } else {
      body.append(
        note('This build carries no GitHub client ID, so browser sign-in is unavailable. Paste a token below.', 'info'),
      );
    }

    // The token route is always available: it is the fallback when the browser
    // flow is not configured, and the only route for org repos that block it.
    const form = element<HTMLDivElement>(`
      <div class="sheet-field">
        <label class="sheet-label" for="gh-token">Personal access token</label>
        <input class="sheet-input" id="gh-token" type="password" spellcheck="false"
               autocomplete="off" placeholder="ghp_… or github_pat_…" />
        <p class="sheet-hint">Needs the <code>repo</code> scope. Stored in your system keychain, never on disk.</p>
      </div>
    `);
    const input = form.querySelector<HTMLInputElement>('input')!;
    body.append(form);

    const submit = async () => {
      const next = await GitHubUseToken(input.value);
      onChanged(next);
      if (next.connected) {
        close();
        return;
      }
      render(next);
    };
    input.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') {
        event.preventDefault();
        void submit();
      }
    });

    const actions = element<HTMLDivElement>('<div class="sheet-actions"></div>');
    actions.append(button('Cancel', 'btn btn-quiet', close), button('Connect', 'btn', () => void submit()));
    panel.append(actions);
    input.focus();
  };

  render(await GitHubAccount());
}

/** Runs the device flow: show a code, wait for the browser, report the result. */
async function browserFlow(
  panel: HTMLElement,
  onChanged: (account: main.GitHubAccount) => void,
  close: () => void,
  render: (account: main.GitHubAccount) => void,
) {
  const login = await GitHubStartLogin();
  if (login.error) {
    render({ connected: false, login: '', name: '', deviceFlow: false, error: login.error } as main.GitHubAccount);
    return;
  }

  panel.replaceChildren(
    element(`
      <div class="sheet-body">
        <h2 class="sheet-title">Enter this code on GitHub</h2>
        <p class="device-code">${escape(login.userCode)}</p>
        <p class="sheet-text">Waiting for you to authorize in the browser…</p>
      </div>
    `),
  );

  const actions = element<HTMLDivElement>('<div class="sheet-actions"></div>');
  actions.append(
    button('Cancel', 'btn btn-quiet', () => {
      void GitHubCancelLogin();
      close();
    }),
    button('Copy code', 'btn btn-quiet', () => void ClipboardSetText(login.userCode)),
    button('Open GitHub', 'btn', () => BrowserOpenURL(login.verificationUri)),
  );
  panel.append(actions);

  // Opening the browser straight away saves a click; the code is on screen
  // either way, so nothing is lost if the browser does not come forward.
  BrowserOpenURL(login.verificationUri);

  const account = await GitHubAwaitLogin();
  onChanged(account);
  if (account.connected) {
    close();
    return;
  }
  render(account);
}

// --- picking a repository ----------------------------------------------------

/**
 * Shows the repository picker.
 *
 * A manual clone URL sits alongside the list because the list only covers what
 * the API returns: an organisation repo, or one reachable by URL but not by
 * affiliation, would otherwise be unreachable.
 */
export async function openGitHubRepository(onOpened: (info: main.ProjectInfo) => void) {
  const { panel, close } = modal('Open a GitHub repository');

  panel.append(
    element(`
      <div class="sheet-body">
        <h2 class="sheet-title">Open a repository</h2>
      </div>
    `),
  );

  const search = element<HTMLInputElement>(
    '<input class="palette-input" type="text" spellcheck="false" autocomplete="off" placeholder="Search your repositories, or paste a clone URL…" />',
  );
  const list = element<HTMLUListElement>('<ul class="palette-list" role="listbox"></ul>');
  const status = element<HTMLParagraphElement>('<p class="palette-empty">Loading your repositories…</p>');
  panel.append(search, list, status);
  search.focus();

  const clone = async (cloneURL: string) => {
    status.hidden = false;
    status.textContent = `Cloning ${cloneURL}…`;
    list.replaceChildren();

    const info = await CloneRepository(cloneURL);
    if (info.error) {
      status.textContent = info.error;
      return;
    }
    close();
    onOpened(info);
  };

  let repositories: forge.Repository[] = [];
  let rows: forge.Repository[] = [];
  let active = 0;

  const render = () => {
    list.replaceChildren();
    const query = search.value.trim();

    // A pasted URL is an instruction, not a search: offer it directly.
    if (looksLikeCloneURL(query)) {
      status.hidden = false;
      status.textContent = 'Press Enter to clone this URL.';
      return;
    }

    rows = rank(repositories, query);
    status.hidden = rows.length > 0;
    if (rows.length === 0 && repositories.length > 0) status.textContent = 'Nothing matches.';

    rows.forEach((repository, index) => {
      const item = element<HTMLLIElement>('<li class="palette-row" role="option"></li>');
      if (index === active) item.classList.add('active');

      const title = document.createElement('span');
      title.className = 'palette-title';
      const match = query ? fuzzy(repository.fullName, query) : null;
      title.append(...highlight(repository.fullName, match?.positions ?? []));
      item.append(title);

      if (repository.description) {
        const detail = document.createElement('span');
        detail.className = 'palette-detail';
        detail.textContent = repository.description;
        item.append(detail);
      }
      if (repository.private) {
        const hint = document.createElement('span');
        hint.className = 'palette-hint';
        hint.textContent = 'private';
        item.append(hint);
      }

      item.addEventListener('mousedown', (event) => {
        event.preventDefault();
        void clone(repository.cloneUrl);
      });
      list.append(item);
    });
    list.children[active]?.scrollIntoView({ block: 'nearest' });
  };

  search.addEventListener('input', () => {
    active = 0;
    render();
  });
  search.addEventListener('keydown', (event) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (rows.length === 0) return;
      active = (active + (event.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length;
      render();
      return;
    }
    if (event.key !== 'Enter') return;
    event.preventDefault();

    const query = search.value.trim();
    if (looksLikeCloneURL(query)) {
      void clone(query);
      return;
    }
    if (rows[active]) void clone(rows[active].cloneUrl);
  });

  const listed = await GitHubRepositories();
  if (listed.error) {
    // Not fatal: a clone URL still works without an account, and saying so
    // beats an empty list with no explanation.
    status.textContent = `${listed.error}. You can still paste a clone URL.`;
    return;
  }
  repositories = listed.repositories ?? [];
  if (repositories.length === 0) status.textContent = 'No repositories found. Paste a clone URL instead.';
  render();
}

function rank(repositories: forge.Repository[], query: string): forge.Repository[] {
  if (!query) return repositories.slice(0, 200);

  return repositories
    .map((repository) => ({ repository, match: fuzzy(repository.fullName, query) }))
    .filter((scored) => scored.match !== null)
    .sort((a, b) => b.match!.score - a.match!.score)
    .slice(0, 200)
    .map((scored) => scored.repository);
}

/** Whether the text is a clone URL rather than a search term. */
function looksLikeCloneURL(text: string): boolean {
  // file:// is included because git treats it as a clone URL like any other,
  // and cloning a local repository is a legitimate thing to want.
  return /^(https?:\/\/|git@|ssh:\/\/|file:\/\/)/.test(text);
}

// --- small builders ----------------------------------------------------------

function button(label: string, className: string, onClick: () => void): HTMLButtonElement {
  const element = document.createElement('button');
  element.className = className;
  element.textContent = label;
  element.addEventListener('click', onClick);
  return element;
}

function note(text: string, kind: 'bad' | 'info'): HTMLParagraphElement {
  const paragraph = document.createElement('p');
  paragraph.className = `sheet-note ${kind}`;
  paragraph.textContent = text;
  return paragraph;
}

/** Escapes text going into an innerHTML template. */
function escape(text: string): string {
  const node = document.createElement('span');
  node.textContent = text;
  return node.innerHTML;
}
