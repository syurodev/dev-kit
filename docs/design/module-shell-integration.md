# Tích hợp module với sidebar và tab strip của app shell

Áp dụng khi tạo hoặc sửa UI module. Đây là hợp đồng tích hợp hiện tại; các spec/plans lịch sử không thay thế API runtime. Cập nhật tài liệu này khi thay đổi hợp đồng shell. Quy tắc trình bày bổ sung nằm tại [UI/UX rules](./devkit-ui-ux-rules.md).

## 1. Ai sở hữu phần nào?

| Phần | Chủ sở hữu | Module phải dùng |
|---|---|---|
| Cột sidebar: vị trí, chiều rộng, resize, toggle, clipping | App shell | `SidebarSurface`; module chỉ cung cấp nội dung |
| Danh sách/tree, search, filter và action trong rail | Module, dùng primitive chung | `SidebarHeader`, `SidebarSearch`, `SidebarSection`, `SidebarRow`/`TreeView`, `SidebarInset` |
| Document tab strip: mở/đóng, focus, pin, reorder, split, persistence | Workbench | `ModuleManifest.tabs`, `newTab`, `useWorkbenchTabsStore` |
| Nội dung một document | Module view | Nhận ID từ `params`; render đúng đối tượng đó |
| Tabs chế độ bên trong cùng một đối tượng | Module | Shadcn `Tabs`, ví dụ Terminal/Commands/Tunnels/Details của một connection |
| Draft và tài nguyên đang chạy | Store/service phù hợp với lifecycle | Không dựa vào việc view còn mounted để giữ công việc |

**Không dựng thêm `<aside>` cố định hoặc một connection/document tab strip trong module view.** Điều hướng giữa các đối tượng thuộc shell; shadcn Tabs chỉ dùng cho các chế độ thực sự bên trong một document. Nếu cần thay đổi hành vi sidebar/strip, sửa primitive chung để mọi module nhận cùng hành vi.

## 2. Khai báo rail ở đâu?

Ưu tiên `ModuleManifest.sidebar` cho danh sách thuộc module: connections, notes, sessions, projects. Rail phải dùng được kể cả khi không có document nào mở. Khai báo lazy để không kéo UI nặng vào startup graph.

SSH là ví dụ đang chạy, xem [manifest](../../frontend/src/modules/ssh/manifest.ts):

```tsx
sidebar: lazy(() =>
  import("./SSHSidebar").then(({ SSHSidebar }) => ({ default: SSHSidebar })),
),
tabs: true,
newTab: () => useSSHStore.getState().setShowNewConnection(true),
```

`newTab` phải làm một việc cụ thể: tạo document mới hoặc mở form tạo đối tượng. Form tạo connection cần thuộc rail/controller còn tồn tại khi đóng mọi document; không để nút + phụ thuộc callback của một view đã unmount.

Dùng `useSidebarContent(node)` khi rail phụ thuộc document hiện tại. Hook lấy module ID từ `SidebarModuleIdContext` do workbench cung cấp, đăng ký theo registrar và tự cleanup. Đăng ký **không phụ thuộc toggle sidebar hoặc document đang visible**; shell quyết định rail nào được hiển thị. Không gọi trực tiếp store để ghi một slot global và không tự clear rail của các document khác. Không render node đó lần nữa bên trong view.

`SidebarSurface` ưu tiên rail được đăng ký qua hook, sau đó mới fallback về rail trong manifest. Chỉ khai báo manifest nếu rail là thuộc module; không đăng ký cùng một rail từ mọi view chỉ để nó xuất hiện.

## 3. Hình dạng nội dung sidebar

```tsx
<div className="flex h-full min-h-0 flex-col">
  <SidebarHeader>
    <SidebarSearch {...searchProps} />
    <Button variant="ghost" size="icon-sm" aria-label="New connection">
      <Plus />
    </Button>
  </SidebarHeader>
  <div className="min-h-0 flex-1 overflow-y-auto">
    <SidebarSection label="Connections" first />
    {/* Rows/tree; row primitive owns padding and hit target. */}
  </div>
</div>
```

Đây là khung bố cục, không phải component hoàn chỉnh. `searchProps`, handler tạo đối tượng và dữ liệu do module cung cấp.

- Không đặt `w-64`, `min-w-*`, resize handle hoặc toggle riêng vào root rail; shell đã quản lý geometry và layout pressure.
- Root rail không có nền, border hoặc shadow riêng. `.shell-sidebar` hòa vào nền opaque của app shell ở cả dark/light, kể cả chế độ giảm transparency. Search/control/selected row vẫn dùng token theme; không xóa feedback selection.
- Root giữ `min-h-0`, body cuộn `flex-1`. Header/action/footer cố định ở ngoài viewport.
- `SidebarHeader` đã dùng `SidebarInset`; không bọc thêm inset. Viewport cuộn chạm mép rail; đặt inset bên trong cho form/card, không bao quanh cả scroller.
- `TreeView` có viewport/virtualization riêng: không thêm một scroller ngoài nó. Không thu nhỏ chữ hoặc hit target để bù cho padding chồng nhau.
- Dùng shadcn/shared primitives sẵn có cho Button, Tooltip, Empty, Field và feedback; wrapper mỏng chỉ compose hành vi của module.

## 4. Mở document qua workbench

Một đối tượng có một ID ổn định trong `params`. Sidebar, palette, toast action và các lối mở khác gọi cùng helper `documents.ts`; không duy trì `openIds`/`selectedId` song song trong module store.

Ví dụ từ [SSH documents](../../frontend/src/modules/ssh/documents.ts):

```tsx
useWorkbenchTabsStore.getState().openTab({
  moduleId: "ssh",
  viewId: "ssh.connections",
  title: profile.name,
  icon: Terminal,
  params: { profileId: profile.id },
  preview: false,
});
```

View nhận `params?: Record<string, unknown>`, kiểm tra kiểu `profileId`, rồi tải đúng profile. ID thiếu hoặc đối tượng đã bị xóa phải có empty/unavailable state; không âm thầm mở profile đầu tiên.

`openTab` tạo identity từ module/view/params và focus tab đã tồn tại khi mở lại. Không đưa title, timestamp, command hoặc output vào params để ép tạo tab mới. Params được persist: chỉ chứa định danh/metadata điều hướng không nhạy cảm, không chứa secret, private key, token hoặc nội dung kết quả.

| Lựa chọn | Khi dùng |
|---|---|
| `tabs: true` | Module có nhiều document/đối tượng có thể mở cùng lúc |
| `newTab` | Hành động cụ thể của nút +, vẫn hoạt động khi không có document |
| `preview: true` | Một slot xem thử có thể bị thay khi chọn đối tượng khác |
| `preview: false` | Document người dùng muốn giữ mở; SSH dùng một tab giữ mở cho mỗi connection |
| `opensHome: true` | Home là dashboard/list hữu ích, không tạo công việc mới khi vào module |
| Không bật `opensHome` | Để shell hiện empty state và rail; không tự tạo connection/console/document |

Không render `TabStrip` trực tiếp trong module; workbench render strip cho từng document group. Shell đã xử lý shortcut, roving focus, menu Close/Pin, reorder và split. Không tự viết lại các hành vi này bằng một mảng button/Tabs.

Sidebar highlight và tác vụ nền phải đọc document của **focused group**, không chỉ `modules[id].activeTabId`: khi split, nhiều group có thể hiện cùng module. Xem `activeSSHProfileId` trong helper SSH. Đổi tên cập nhật title trong tab hiện có, giữ ID/group/pin; xóa đối tượng đóng các document của đối tượng đó và cleanup tài nguyên theo contract của module.

## 5. Lifecycle và dialog

Đổi module không đồng nghĩa đóng document; đóng document cũng không mặc nhiên kết thúc một tài nguyên chạy nền. Với SSH:

- Đóng connection document giữ draft/output trong bộ nhớ và terminal/tunnel đang chạy.
- Đóng terminal riêng kết thúc session; sửa/xóa profile cleanup sessions/tunnels của profile theo contract hiện có.
- Callback hoàn tất chạy nền có thể tồn tại sau khi document đóng. Toast action phải gọi helper mở đúng document và đúng subview, không mở một home view chung.
- Draft nhạy cảm giữ trong store ephemeral; không persist command/output vào UI preferences chỉ để giữ UI.

View có thể bị hidden, unmount do chính sách mount của workbench, rồi mount lại. Không đặt vòng đời process/socket/terminal vào cleanup của document view nếu contract yêu cầu nó tiếp tục chạy.

Dùng `useModuleVisibility()` cho dialog/portal thuộc document. Một tab đang mounted nhưng hidden không được hiện approval/edit/delete dialog lên tab đang xem. Rail thuộc manifest đọc module đang active cho dialog của rail. Khi cần giữ draft form qua việc ẩn dialog, giữ component mounted và điều khiển `open`/`visible`, không xóa state form.

Đọc store qua selector hoặc `useShallow`; tránh subscribe toàn store khiến mỗi thao tác tab re-render mọi document/rail.

## 6. Checklist triển khai và review

1. Sidebar đến từ manifest hoặc `useSidebarContent`, không có cột `<aside>` thứ hai trong view.
2. Rail vẫn có khi đóng mọi tab; toggle/resize của app shell điều khiển nó; không có nền/outline riêng.
3. Có helper mở document bằng ID ổn định; không có danh sách tab/selection thứ hai trong module store.
4. Nút + hoạt động khi module không có document; mở lại cùng đối tượng focus tab cũ.
5. Pin, close, reorder, keyboard và split dùng strip chung; sidebar selection theo focused group.
6. Rename giữ tab identity/pin; delete không để document mồ côi; close document và close tài nguyên có ý nghĩa rõ ràng.
7. Đổi tab/module, đóng rồi mở lại không làm mất draft hay vô tình kết thúc công việc nền.
8. Dialog của tab/module nền không xuất hiện lên tab khác; toast quay về đúng document.
9. Kiểm tra dark/light, sidebar hẹp/rộng, scrollbar, empty/loading/error và params restore khi đối tượng đã bị xóa.
10. Chạy frontend tests/build và architecture gate theo `AGENTS.md`; bổ sung regression test cho lifecycle/identity thực sự thay đổi. Browser harness mô phỏng không thay thế smoke test native/live services.

## Nguồn runtime để đối chiếu

- [ModuleManifest và view contract](../../frontend/src/modules/registry.ts)
- [SidebarSurface, hook và chrome primitives](../../frontend/src/components/ui/sidebar-shell.tsx)
- [Sidebar registration ownership](../../frontend/src/stores/sidebarContent.ts)
- [Workbench rendering và visibility](../../frontend/src/workbench/Workbench.tsx)
- [Workbench tab state](../../frontend/src/stores/workbenchTabs.ts) và [TabStrip](../../frontend/src/workbench/tabs/TabStrip.tsx)
- [SSH sidebar](../../frontend/src/modules/ssh/SSHSidebar.tsx), [document view](../../frontend/src/modules/ssh/SSHView.tsx), [document regression tests](../../frontend/src/modules/ssh/documents.test.ts)

Các trang [Sidebar](../dev-kit/sidebar.mdx), [Module tabs](../dev-kit/module-tabs.mdx) và [Layout](../dev-kit/layout.mdx) có thêm bối cảnh thiết kế; các phần ghi là lịch sử phải đối chiếu với runtime và hướng dẫn này trước khi áp dụng.
