# 城市之眼 Design System

## 1. Atmosphere & Identity

城市之眼是一個溫暖、清楚、可快速操作的無障礙公車助手。畫面以米白背景和深色文字降低視覺疲勞，互動使用琥珀色建立方向感；產品簽名是「安全視野窗」的琥珀色高亮，讓重要的公車資訊在一眼內被辨識。介面以邊框、留白和明確的狀態文字建立層次，不依賴裝飾性動畫。

## 2. Color

### Palette

| Role | Token | Value | Usage |
|------|-------|-------|-------|
| Background | `--color-bg` | `#F7F5F0` | Page background |
| Surface | `--color-surface` | `#FFFFFF` | Cards and controls |
| Surface muted | `--color-surface-muted` | `#ECE9E2` | Secondary panels and file icon well |
| Text primary | `--color-text` | `#14181F` | Headings and body copy |
| Text muted | `--color-text-muted` | `#5B6270` | Hints and secondary information |
| Border | `--color-border` | `#D8D4CA` | Control and card outlines |
| Accent primary | `--color-primary` | `#C8850C` | Main actions and safe-zone emphasis |
| Accent primary text | `--color-primary-text` | `#14181F` | Text/icons on primary actions |
| Accent secondary | `--color-secondary` | `#1D5FB8` | Focus rings and secondary actions |
| Status error | `--color-danger` | `#C62828` | Error messages |
| Status success | `--color-success` | `#1E8E3E` | Successful completion |
| Safe-zone background | `--safe-zone-bg` | `rgba(200, 133, 12, 0.12)` | Product signature highlight |
| Safe-zone border | `--safe-zone-border` | `#C8850C` | Product signature outline |

Rules: new UI uses tokens rather than new raw colors. Accent is reserved for actions, focus, status, and the safe-zone signature.

## 3. Typography

### Scale

| Level | Token | Size | Weight | Usage |
|-------|-------|------|--------|-------|
| XL | `--font-xl` | `2rem * --font-scale` | 700 | Large result and page title |
| LG | `--font-lg` | `1.5rem * --font-scale` | 700 | Section heading and card emphasis |
| MD | `--font-md` | `1.125rem * --font-scale` | 600-700 | Primary controls and readable hints |
| SM | `--font-sm` | `0.875rem * --font-scale` | 400-600 | Secondary copy and status |
| XS | `--font-xs` | `0.75rem * --font-scale` | 400 | Supporting metadata |

Font stack: `-apple-system`, `BlinkMacSystemFont`, `Noto Sans TC`, `PingFang TC`, sans-serif. The global `--font-scale` is user-adjustable and must remain respected by new controls.

## 4. Spacing & Layout

The existing product uses an 8px rhythm:

| Token | Value | Usage |
|-------|-------|-------|
| `--space-1` | 8px | Inline gaps and compact padding |
| `--space-2` | 16px | Control groups and card padding |
| `--space-3` | 24px | Section spacing and generous padding |
| `--space-4` | 32px | Large separation |
| `--space-5` | 48px | Major page spacing |

Passenger content is a single-column stack capped at 480px; driver content is capped at 560px. All primary controls span the available column, use intrinsic wrapping, and remain usable at 375px without horizontal scrolling. Touch targets use `--touch-min: 48px`.

## 5. Components

### Button

- **Structure**: native `<button>` with `.btn`, `.btn-primary`, or `.btn-secondary`.
- **Variants**: primary full-width action; secondary outlined action; disabled; done.
- **Spacing**: minimum 48px touch height; primary uses `--space-1` internal gap.
- **States**: default, hover, active, focus-visible, disabled, loading, done, error via nearby status text.
- **Accessibility**: keyboard reachable, visible focus ring, native button semantics, no icon-only action without an accessible label.
- **Motion**: 150-200ms color/opacity feedback; reduced motion collapses transitions.
- **Layout**: stack/cluster primitive depending on parent.

### Camera capture

- **Structure**: `.camera-wrap` video preview, `.camera-hint`, `.camera-status`, `.shutter-btn`.
- **Variants**: idle, scanning, permission/error.
- **Spacing**: `--space-2` between preview and hint; `--space-3` below shutter control.
- **States**: idle, scanning pulse, success result, error message.
- **Accessibility**: camera status is surfaced in text; shutter has an explicit accessible label.
- **Motion**: scanning pulse is meaningful feedback and is disabled by reduced-motion rules.
- **Layout**: vertical page stack.

### Safe-zone result

- **Structure**: `.safe-zone-window` with route, ETA, direction, expandable bus list, and actions.
- **Variants**: populated, expanded list, notification complete.
- **Spacing**: `--space-2` between result blocks; `--space-3` inside the highlighted panel.
- **States**: default, expanded, action done, empty/error from the API.
- **Accessibility**: native buttons and visible focus; result text remains readable at the user's font scale.
- **Motion**: only state feedback; no decorative motion.
- **Layout**: vertical stack with a two-column action cluster.

### Manual photo upload

- **Structure**: visually labelled native file input, `.manual-upload` action row, explicit test-location checkbox, selected file metadata, and live status text.
- **Variants**: idle, test GPS enabled, live GPS enabled, file selected, uploading, success, error.
- **Spacing**: action row uses `--space-1`; file metadata sits in a muted surface with `--space-2` padding.
- **States**: default button, focus-visible, disabled while uploading, success confirmation, error with retry through selecting another file.
- **Accessibility**: the upload button and test-location checkbox are keyboard reachable; the hidden image-only input stays out of the tab order (`tabindex="-1"`); status uses `aria-live="polite"`.
- **Motion**: button/status transition uses the existing 150-200ms feedback timing; reduced motion keeps the state change without movement.
- **Layout**: compact cluster below the camera capture control; the test-location row wraps its coordinate copy naturally at narrow widths.

## 6. Motion & Interaction

| Type | Duration | Easing | Usage |
|------|----------|--------|-------|
| Micro | 150ms | ease-out | Button press and status color |
| Standard | 200ms | ease-in-out | Toggle and panel state |
| Emphasis | 300ms | ease-out | Result/state reveal |
| Scanning | 1500ms loop | ease-in-out | Camera scanning pulse only |

Only transform, opacity, and color feedback are animated for new interactions. `prefers-reduced-motion: reduce` removes non-essential movement while preserving state and status changes. The manual upload flow follows the beui.dev file-upload mechanism: a clear browse action, explicit queued/uploading/success/error states, a visible retry path through selecting another file, and a checked-by-default test GPS toggle for offline station testing. Unchecking the toggle restores real device GPS.

## 7. Depth & Surface

Strategy: mixed, border-first. Cards and controls use the shared 2px border and warm surface hierarchy; the existing switch thumb uses a small shadow to communicate elevation. New upload controls use the same border/surface language and do not introduce a new shadow recipe.

## 8. Accessibility Constraints & Accepted Debt

### Constraints

- WCAG 2.2 AA target.
- Body text uses the existing scalable font tokens; no new text below `--font-xs` for primary content.
- Every interactive control has a visible `:focus-visible` outline.
- Every touch action is at least `--touch-min` where practical.
- File upload accepts images only and announces status changes with `aria-live`.
- Manual testing uses the visible `新益里` test GPS by default; live GPS is opt-in by unchecking the test-location checkbox.
- Respect `prefers-reduced-motion`.
- Preserve CJK natural wrapping and prevent horizontal overflow at 375px.

### Accepted Debt

| Item | Location | Why accepted | Owner / Exit |
|------|----------|--------------|--------------|
| Existing emoji text icons in result actions | `templates/passenger.html` | Pre-existing surface; outside this small upload feature | Replace with SVG icon set during a dedicated icon/accessibility pass |
| Inline style declarations in calibration hints | `templates/passenger.html` | Pre-existing layout-only declarations | Consolidate during a broader template cleanup |
