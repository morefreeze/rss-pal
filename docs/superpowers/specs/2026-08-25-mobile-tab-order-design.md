# Mobile Tab Order Design

## Goal

Make the mobile bottom navigation expose the primary destinations in the
requested order while folding the remaining destinations into the existing
More sheet.

## Navigation Order

The complete mobile navigation order is:

1. 文章
2. 网摘
3. 订阅
4. 简报
5. 兴趣
6. 统计
7. 设置

The fixed bottom tab bar shows `文章`, `网摘`, `订阅`, `简报`, and `更多`.
Opening `更多` shows `兴趣`, `统计`, `设置`, and then the existing `登出`
action.

## Implementation

- Add the existing `/briefing` destination to `MobileTabBar` after `/feeds`.
- Preserve the current `MoreSheet` destinations and order.
- Keep unread badges and the special article/net-clip active-state behavior
  unchanged.
- Do not change desktop navigation, routes, page behavior, or logout behavior.

## Testing

- Add a mobile navigation test that renders the real `MobileTabBar` and asserts
  the bottom controls appear in the exact order `文章`, `网摘`, `订阅`, `简报`,
  `更多`.
- Assert the open More sheet presents `兴趣`, `统计`, `设置`, `登出` in that
  exact order.
- Run the full frontend check and production build before integration.

## Out of Scope

- Responsive or user-configurable tab counts.
- Refactoring desktop and mobile navigation into a shared configuration.
- Visual restyling of the tab bar or More sheet.
