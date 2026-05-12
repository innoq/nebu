---
name: template-implementation
code: template-implementation
description: Write or update Go Templates for Admin UI features using Tailwind and DaisyUI. Accessibility-correct from the first line.
---

# Template Implementation

## What Success Looks Like

The template renders the intended UI, uses established DaisyUI components correctly, passes accessibility requirements from the PRD, and makes the Playwright tests go from red to green.

## Before Writing

Read MEMORY.md for:
- Established component patterns (which DaisyUI components are used and how)
- Go Template layout conventions (which base template to extend, how partials work)
- PRD accessibility requirements that apply

Look at existing templates in `gateway/internal/admin/` for conventions before inventing new ones.

## Implementation Standards

### Go Template Structure

```html
{{template "layout" .}}

{{define "content"}}
<main id="main-content"> <!-- id for skip-to-content link -->
  <!-- Page content here -->
</main>
{{end}}
```

### Accessible Interactive Elements

```html
<!-- Button — always use <button>, not <div> or <a> for actions -->
<button type="button" class="btn btn-primary" aria-label="[if no visible text]">
  Label text
</button>

<!-- Form input — always with associated label -->
<div class="form-control">
  <label class="label" for="field-id">
    <span class="label-text">Field label</span>
  </label>
  <input id="field-id" type="text" class="input input-bordered"
         aria-describedby="field-id-error" />
  <span id="field-id-error" class="label-text-alt text-error" role="alert">
    {{if .Error}}{{.Error}}{{end}}
  </span>
</div>

<!-- Navigation -->
<nav aria-label="[descriptive label]">
  <ul>
    <li><a href="/admin/rooms" {{if .Active.Rooms}}aria-current="page"{{end}}>Rooms</a></li>
  </ul>
</nav>
```

### DaisyUI Component Usage

Use DaisyUI semantic classes:
- Buttons: `btn`, `btn-primary`, `btn-secondary`, `btn-ghost`, `btn-sm`
- Cards: `card`, `card-body`, `card-title`
- Tables: `table`, `table-zebra`
- Badges: `badge`, `badge-primary`, `badge-error`
- Alerts: `alert`, `alert-success`, `alert-error`
- Forms: `form-control`, `input`, `input-bordered`, `select`, `textarea`
- Loading: `loading`, `loading-spinner`

Do not use hardcoded Tailwind colors (e.g., `text-blue-500`) where DaisyUI theme variables exist (`text-primary`, `text-error`).

### Responsive Design

Mobile-first. Use Tailwind responsive prefixes when needed: `sm:`, `md:`, `lg:`. Admin UI minimum viewport: 768px, but must not break on smaller.

## Memory Integration

After implementing: if a new reusable component pattern was established, add it to the session log for curation into MEMORY.md.
