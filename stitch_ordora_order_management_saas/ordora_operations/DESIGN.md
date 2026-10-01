---
name: Ordora Operations
colors:
  surface: '#f8f9ff'
  surface-dim: '#cbdbf5'
  surface-bright: '#f8f9ff'
  surface-container-lowest: '#ffffff'
  surface-container-low: '#eff4ff'
  surface-container: '#e5eeff'
  surface-container-high: '#dce9ff'
  surface-container-highest: '#d3e4fe'
  on-surface: '#0b1c30'
  on-surface-variant: '#464555'
  inverse-surface: '#213145'
  inverse-on-surface: '#eaf1ff'
  outline: '#777587'
  outline-variant: '#c7c4d8'
  surface-tint: '#4d44e3'
  primary: '#3525cd'
  on-primary: '#ffffff'
  primary-container: '#4f46e5'
  on-primary-container: '#dad7ff'
  inverse-primary: '#c3c0ff'
  secondary: '#565e74'
  on-secondary: '#ffffff'
  secondary-container: '#dae2fd'
  on-secondary-container: '#5c647a'
  tertiary: '#005338'
  on-tertiary: '#ffffff'
  tertiary-container: '#006e4b'
  on-tertiary-container: '#67f4b7'
  error: '#ba1a1a'
  on-error: '#ffffff'
  error-container: '#ffdad6'
  on-error-container: '#93000a'
  primary-fixed: '#e2dfff'
  primary-fixed-dim: '#c3c0ff'
  on-primary-fixed: '#0f0069'
  on-primary-fixed-variant: '#3323cc'
  secondary-fixed: '#dae2fd'
  secondary-fixed-dim: '#bec6e0'
  on-secondary-fixed: '#131b2e'
  on-secondary-fixed-variant: '#3f465c'
  tertiary-fixed: '#6ffbbe'
  tertiary-fixed-dim: '#4edea3'
  on-tertiary-fixed: '#002113'
  on-tertiary-fixed-variant: '#005236'
  background: '#f8f9ff'
  on-background: '#0b1c30'
  surface-variant: '#d3e4fe'
typography:
  display-lg:
    fontFamily: Inter
    fontSize: 32px
    fontWeight: '700'
    lineHeight: 40px
    letterSpacing: -0.025em
  headline-lg:
    fontFamily: Inter
    fontSize: 24px
    fontWeight: '600'
    lineHeight: 32px
    letterSpacing: -0.02em
  headline-lg-mobile:
    fontFamily: Inter
    fontSize: 20px
    fontWeight: '600'
    lineHeight: 28px
    letterSpacing: -0.015em
  headline-md:
    fontFamily: Inter
    fontSize: 18px
    fontWeight: '600'
    lineHeight: 24px
    letterSpacing: -0.015em
  body-lg:
    fontFamily: Inter
    fontSize: 15px
    fontWeight: '400'
    lineHeight: 22px
    letterSpacing: -0.01em
  body-md:
    fontFamily: Inter
    fontSize: 13px
    fontWeight: '400'
    lineHeight: 18px
    letterSpacing: -0.005em
  body-sm:
    fontFamily: Inter
    fontSize: 12px
    fontWeight: '400'
    lineHeight: 16px
    letterSpacing: 0em
  label-md:
    fontFamily: Inter
    fontSize: 12px
    fontWeight: '600'
    lineHeight: 16px
    letterSpacing: 0.01em
  label-sm:
    fontFamily: Inter
    fontSize: 11px
    fontWeight: '500'
    lineHeight: 14px
    letterSpacing: 0.02em
  code-md:
    fontFamily: Inter
    fontSize: 12px
    fontWeight: '500'
    lineHeight: 16px
    letterSpacing: -0.01em
rounded:
  sm: 0.125rem
  DEFAULT: 0.25rem
  md: 0.375rem
  lg: 0.5rem
  xl: 0.75rem
  full: 9999px
spacing:
  gutter: 1rem
  gutter-desktop: 1.5rem
  margin: 1rem
  margin-desktop: 2rem
  space-xs: 0.25rem
  space-sm: 0.5rem
  space-md: 0.75rem
  space-lg: 1.25rem
  space-xl: 2rem
---

## Brand & Style

The design system is engineered for makers, bespoke fabricators, custom ateliers, and rapid-turnaround repair shops who rely on operational precision. The visual identity reflects disciplined enterprise SaaS: authoritative, meticulously structured, and free of ornamental bloat. Drawing structural rigor from modern developer tools and transaction engines, the interface emphasizes speed, tactile efficiency, and zero ambiguity.

A balance of crisp neutral backgrounds, ultra-precise hairline borders, and high-visibility status indicators transforms complex multi-stage order tracking into a calm, reliable workspace. High-density data tables, monospaced order identifiers, and real-time financial tallies are treated as central interface anchors, ensuring shop managers and operational leads can parse critical paths instantly.

## Colors

The palette leverages a focused indigo core paired with a deep slate infrastructure to establish clear visual hierarchy.

- **Primary (`#4F46E5`)**: Reserved for active interface controls, interactive accents, primary submission actions, and selected navigational states.
- **Secondary (`#0F172A`)**: The deep slate foundation for dominant headers, strong high-contrast badges, and primary text values.
- **Semantic Accents**:
  - **Success / Settled (`#10B981` / `#059669`)**: Confirmed payments, completed work orders, and positive balance states.
  - **Warning / Progress (`#F59E0B` / `#D97706`)**: Active queue items, stage-in-progress indicators, and deadlines due within 24 hours.
  - **Critical / Overdue (`#EF4444` / `#DC2626`)**: Deficits, past-due deliverables, and blocked order workflows.
- **Surfaces & Borders**:
  - Application Canvas: `#F8FAFC`
  - Elevated Container / Cards: `#FFFFFF`
  - Hairline Boundaries & Dividers: `#E2E8F0`
  - Subdued Field Fill: `#F1F5F9`

## Typography

The typography system relies exclusively on `Inter`, utilizing tight letter-spacing and varied font weights to ensure exceptional scanability across high-density layouts.

- **Tabular Figures & Metrics**: Numeric content—such as SKU codes, order references, currency amounts, and fulfillment lead times—must enable CSS `font-variant-numeric: tabular-nums` to maintain aligned columns.
- **Visual Scale Hierarchy**:
  - Page headers use `headline-lg` with tight tracking (`-0.02em`) to ground dashboard surfaces.
  - Core operational data leverages `body-md` (13px) to maximize row efficiency without sacrificing readability.
  - Metadata labels, timestamps, and column headers use `label-sm` or `label-md` with slight positive tracking for clear demarcation.

## Layout & Spacing

The layout model enforces a strictly aligned structure centered around responsive fluidity and information density:

- **Desktop (1024px+)**: A 12-column grid utilizing a dynamic fluid canvas with a maximum container limit of `1440px` for operations management, scaling down with fixed 256px side-navigation channels. Gutters are locked to `1.5rem` (`gutter-desktop`), with margins set to `2rem` (`margin-desktop`).
- **Tablet (768px - 1023px)**: Condenses to an 8-column system with `1rem` gutters; secondary panels collapse into off-canvas sliding drawers.
- **Mobile (< 768px)**: 4-column system with `1rem` outer canvas padding. Tables pivot to swipeable stacked operational cards.
- **Internal Density**: Components utilize condensed step values (`space-xs` = 4px, `space-sm` = 8px, `space-md` = 12px) to optimize screen real estate and display complete order lifecycles above the fold.

## Elevation & Depth

Visual separation relies on crisp surface transitions and hairline borders rather than deep drop shadows.

- **Subtle Boundaries**: Every card, table, and header container utilizes a crisp `1px` solid border (`#E2E8F0`).
- **Surface Layering**: The primary foundation sits at `#F8FAFC`. Primary data cards and working tables sit elevated at `#FFFFFF`.
- **Low-Impact Shadows**:
  - Default cards: `0 1px 2px 0 rgba(15, 23, 42, 0.05)`
  - Active dropdowns, context menus, and slide drawers: `0 4px 6px -1px rgba(15, 23, 42, 0.08), 0 2px 4px -2px rgba(15, 23, 42, 0.04)`
  - Modals and drawers: `0 20px 25px -5px rgba(15, 23, 42, 0.1), 0 8px 10px -6px rgba(15, 23, 42, 0.04)` paired with an unblurred `#0F172A` backdrop at `40%` opacity.

## Shapes

The interface incorporates a disciplined `roundedness: 1` standard to project structured precision.

- **Base Corner Radius (0.25rem / 4px)**: Applied to all table row indicators, text input fields, inline buttons, action dropdown triggers, and status badges.
- **Container Radius (0.5rem / 8px via `rounded-lg`)**: Applied to high-level content cards, modal windows, flyout side panels, and bulk data wrappers.
- **Pill Exceptions**: Small status indicator tags and badge counts utilize full rounded pills (`9999px`) to create an immediate visual distinction between data containers and dynamic status markers.

## Components

### Buttons
- **Primary**: Solid `#4F46E5` fill, `#FFFFFF` text, 0.25rem radius, 8px 14px padding. Hover state: `#4338CA`. Active state: `#3730A3`.
- **Secondary**: `#FFFFFF` background, `1px solid #CBD5E1`, `#0F172A` text. Hover: `#F8FAFC` and border `#94A3B8`.
- **Destructive**: Subdued rose background (`#FEF2F2`), `1px solid #FCA5A5`, `#B91C1C` text. Hover: `#DC2626` text, `#FEE2E2` fill.

### Tables & Dense Lists
- Headers use `11px` bold uppercase text (`#64748B`) with `8px 12px` padding on `#F8FAFC`.
- Rows feature a clean border-bottom (`#E2E8F0`), maintaining a compact height of `40px` to `48px`.
- Numerical columns and monetary values align right and display using tabular numeric figures.

### Status Badges & Dots
- Inline pills with `4px 8px` padding and `11px` weight.
- Include a leading `6px` circular dot:
  - **In Progress / Queued**: Amber text (`#B45309`), background (`#FEF3C7`), dot (`#F59E0B`).
  - **Paid / Complete**: Emerald text (`#047857`), background (`#D1FAE5`), dot (`#10B981`).
  - **Overdue / Action Needed**: Crimson text (`#B91C1C`), background (`#FEE2E2`), dot (`#EF4444`).

### Financial Split Balance Tiles
- Three-part structural segments displaying Total, Paid, and Balance Due.
- Clear typographic separation: small subdued label above, bold primary currency figure below, highlighted with an emerald or crimson indicator depending on outstanding liabilities.

### Input Fields & Controls
- Height fixed at `36px` for operational consistency.
- `#FFFFFF` surface with `1px solid #CBD5E1` border, transitioning to a `2px` focus ring in `#4F46E5` with `0` offset on focus.
- Checkboxes and radios use strict `16px` bounds with high-contrast active states.

### Quick-Action Slide Drawers
- Flyout panels anchored to the right viewport edge (480px fixed desktop width).
- Structured with a sticky order header, scrollable fulfillment checklist body, and a sticky action footer for fast state updates.