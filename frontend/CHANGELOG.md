# Frontend Changelog — DevKit Design System Migration

Tài liệu ghi lại toàn bộ các bước khảo sát, thiết lập và cập nhật hệ thống giao diện Frontend (`dev-kit-app/frontend`) đồng bộ theo Figma Design System của DevKit.

---

## [2026-08-16] — Giai đoạn 1: Khảo sát Figma & Thiết lập Foundation / CSS Tokens

### 1. Trích xuất thông số kỹ thuật từ Figma qua MCP (`figma-dev-mode-mcp-server`)
Đã kết nối trực tiếp với Figma Desktop và trích xuất dữ liệu metadata, variable definitions của các trang từ **00 đến 06**:

* **Page 00: `00 Cover` (`nodeId: 19:2`)**
  * Tôn chỉ thiết kế: *"Quiet power for complex developer work"*.
  * 4 nguyên tắc cốt lõi:
    1. *Opaque by Default:* Nền chính luôn đặc (solid), tối ưu hiệu năng và sự tập trung.
    2. *Glass with Purpose:* Chỉ áp dụng kính mờ cho modal, popover, floating focus layers khi cần phân tầng chiều sâu.
    3. *Lines are Data:* Mọi đường line map trực tiếp vào phân cấp, tiến độ, cuộn hoặc trạng thái (không trang trí thừa).
    4. *Icons Before Words:* Sử dụng Lucide icon cho thao tác quen thuộc; bắt buộc tooltip cho icon-only actions.
  * Signature Palette: Lime, Turquoise, Candy Blue, Lavender, Orchid.

* **Page 01: `01 Foundations` (`nodeId: 22:35`)**
  * **Palette Anchors:** 12 màu gốc (Onyx `#020202`, Carbon `#171717`, Jet Black `#1D1D1D`, Cosmic `#23212C`, Wine Ash `#32292F`, Violet `#36255C`, Lavender `#D2C3F6`, Orchid `#E5BDDF`, Candy Blue `#B2D5E5`, Turquoise `#99E1D9`, Vanilla `#F1FEC8`, Lime `#C6FF34`).
  * **Semantic Tokens:** `background/app`, `background/canvas`, `surface/base`, `surface/raised`, `surface/glass-floating`, `surface/glass-modal`, `action/primary`, `status/info`, `status/success`, `status/warning`, `status/recovery`, `status/danger`.
  * **Typography Scale:** Geist (UI text) & Geist Mono (mã kỹ thuật, recovery key).
  * **4px Layout Grid:** `space/0` (0px) đến `space/12` (48px).
  * **Radius System:** 6px, 8px, 10px (controls), 14px, 18px (containers), 9999px (full).

* **Page 02: `02 Icon` (`nodeId: 22:36`)**
  * Tiêu chuẩn `lucide-react` nét 1.5px stroke.
  * Phân cấp 4 kích thước: 12px (status), 16px (controls), 20px (prominent action), 24px (empty state).

* **Page 03: `03 Button & Icon Button` (`nodeId: 22:37`)**
  * Biến thể: `Primary` (Lime `#C6FF34`), `Secondary` (Dark/Border), `IconButton` (Neutral / Primary).
  * Chiều cao: 28px, 32px (MD), 36px, 40px (LG). Bo góc chuẩn 10px. Focus ring Candy Blue 1px.

* **Page 04: `04 Input & Selection` (`nodeId: 22:38`)**
  * Input: Chiều cao 40px, Persistent Label phía trên, Action slot 16px (toggle mật khẩu), Focus ring Candy Blue 1.5px.
  * Checkbox: Trạng thái unchecked, checked, focus, disabled. Hit target bao trùm cả nhãn và ô chọn.

* **Page 05: `05 Feedback & Progress` (`nodeId: 22:39`)**
  * 4 mẫu Alert: Info, Success, Warning, Error (520x72px).
  * Step Progress: 4 bước nối nhau bằng milestone line 3px (kéo dài bước active).
  * Feedback contract: Thông báo tại chỗ (local surface), hạn chế ngắt quãng toàn cục.

* **Page 06: `06 Security Patterns` (`nodeId: 22:40`)**
  * Recovery Key Field: 3 trạng thái (`Hidden`, `Revealed`, `Copied`).
  * Security Notice: Loại `Secure` và `Irreversible`.
  * Vault Shell: Container tiêu chuẩn 960x540px.

---

### 2. Triển khai Step 1: Cấu hình Foundation & CSS Tokens

#### Thay đổi dependencies:
* Cài đặt thêm `@fontsource-variable/geist-mono` (`^5.3.0`) để đồng bộ font monospace cho recovery key và code blocks.

#### Cập nhật file [`src/index.css`](file:///Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-app/frontend/src/index.css):
* Tích hợp import `@fontsource-variable/geist` và `@fontsource-variable/geist-mono`.
* Khai báo toàn bộ CSS Custom Properties (`:root` và `.dark`) tương ứng 100% với Figma Variables:
  * **Nền & Bề mặt:**
    * `--background-app`: `#020202`
    * `--background-canvas`: `#171717`
    * `--surface-base`: `#1d1d1d`
    * `--surface-raised`: `#23212c`
    * `--surface-glass-floating`: `rgba(35, 33, 44, 0.78)`
    * `--surface-glass-modal`: `rgba(35, 33, 44, 0.92)`
  * **Hành động & Trạng thái:**
    * `--action-primary`: `#c6ff34` (Lime)
    * `--status-info`: `#b2d5e5` (Candy Blue)
    * `--status-success`: `#99e1d9` (Turquoise)
    * `--status-warning`: `#f1fec8` (Vanilla)
    * `--status-recovery`: `#e5bddf` (Orchid)
    * `--status-danger`: `#d87886`
  * **Đường viền & Bán kính:**
    * `--border-default`: `rgba(178, 213, 229, 0.16)`
    * `--border-subtle`: `rgba(178, 213, 229, 0.08)`
    * `--radius-control`: `10px`
    * `--radius-sm`: `6px`, `--radius-md`: `8px`, `--radius-lg`: `14px`, `--radius-xl`: `18px`
* Mapping các biến này vào Tailwind CSS v4 `@theme inline` và hệ thống biến chuẩn của UI library.
* Thiết lập Base layer mặc định Dark-first, phông chữ Geist, khử răng cưa mượt mà.

#### Kiểm thử:
* Đã thực thi `bun run build:dev` (`tsc && vite build --minify false --mode development`), kết quả build thành công 100% không phát sinh lỗi.

---

## [2026-08-16] — Giai đoạn 2: Tùy biến lại các UI Components nguyên tử có sẵn

Đã tùy biến lại toàn bộ các components nền tảng trong `src/components/ui/` theo đúng thông số kỹ thuật chi tiết từ Figma Trang 03 (`Button & Icon Button`) và Trang 04 (`Input & Selection`):

### 1. `Button` (`src/components/ui/button.tsx`)
* **Variants:**
  * `default` / `primary`: Nền `action-primary` (`#c6ff34` - Lime), chữ `action-primary-foreground` (`#020202`), hiệu ứng hover brightness +10%.
  * `secondary`: Nền `surface-raised` (`#23212c`), viền `border-default` (`rgba(178,213,229,0.16)`), chữ `text-primary` (`#f0f1eb`), hover sáng viền `status-info`.
  * `outline`: Nền trong suốt, viền `border-default`, hover `surface-raised`.
  * `ghost`: Nền trong suốt, chữ `text-secondary`, hover `surface-raised`.
  * `destructive`: Nền đỏ dịu `status-danger/15`, viền `status-danger/30`, chữ `status-danger`.
* **Kích thước & Bo góc (Scale):**
  * `xs` / `sm`: Cao 28px (`h-7`), bo góc 8px.
  * `default` / `md`: Cao 32px (`h-8`), bo góc chuẩn **10px** (`rounded-[10px]`), padding ngang 16px.
  * `lg`: Cao 40px (`h-10`), bo góc chuẩn **10px**, padding ngang 18px.
  * `icon` (32x32px), `icon-xs` (24x24px), `icon-sm` (28x28px), `icon-lg` (40x40px).
* **Trạng thái:**
  * `disabled`: Opacity **38%** theo spec Figma, `pointer-events-none`.
  * `focus-visible`: Viền 1px màu `status-info` (`#b2d5e5` Candy Blue).

### 2. `Input` (`src/components/ui/input.tsx`)
* **Kích thước & Bề mặt:**
  * Chiều cao mặc định **40px** (`h-10`), font-size 14px, bo góc 8px.
  * Nền `surface-base` (`#1d1d1d`), viền `border-default`.
  * Placeholder màu `text-tertiary` (`#8b8e8a`).
* **Trạng thái:**
  * `focus-visible`: Viền `1.5px` màu `status-info` (`#b2d5e5` Candy Blue), loại bỏ viền ngoài thô.
  * `aria-invalid` (Error): Viền `status-danger` (`#d87886`).
  * `disabled`: Opacity **38%**, nền `surface-raised` (`#23212c`).

### 3. `Checkbox` (`src/components/ui/checkbox.tsx`)
* **Kích thước:** Chuẩn `18x18px`, bo góc `6px` (`rounded-[6px]`).
* **Nền & Viền:**
  * Trạng thái chưa chọn: Nền `surface-base` (`#1d1d1d`), viền `border-default`.
  * Trạng thái đã chọn (`data-checked`): Nền `action-primary` (`#c6ff34` Lime), icon check đen nét 2.5px.
  * Focus: Viền 1.5px màu `status-info` Candy Blue.
  * Disabled: Opacity **45%**.

### 4. `Tooltip` (`src/components/ui/tooltip.tsx`)
* **Bề mặt Glassmorphism:**
  * Nền kính mờ `surface-glass-floating` (`rgba(35, 33, 44, 0.78)`) kết hợp `backdrop-blur-md`.
  * Viền `border-default` (`rgba(178, 213, 229, 0.16)`), bo góc 8px (`rounded-[8px]`).
  * Chữ `text-primary` (`#f0f1eb`), font-weight medium (12px).

#### Kiểm thử:
* Đã thực thi `bun run build:dev`, toàn bộ component tương thích hoàn toàn, không có lỗi type hoặc CSS.

---

## [2026-08-16] — Giai đoạn 3: Tạo bộ Components Feedback & Security

Đã hoàn thiện bộ component chuyên biệt theo Figma Trang 05 (`Feedback & Progress`) và Trang 06 (`Security Patterns`):

### 1. `Alert` (`src/components/ui/alert.tsx`)
* Hỗ trợ 4 loại trạng thái theo Figma:
  * `info`: Nền `status-info/10`, viền `status-info/70`, icon Candy Blue.
  * `success`: Nền `status-success/10`, viền `status-success/70`, icon Turquoise.
  * `warning`: Nền `status-warning/10`, viền `status-warning/70`, icon Vanilla.
  * `error` / `destructive`: Nền `status-danger/10`, viền `status-danger`, icon đỏ.
* Cấu trúc thẻ: Bo góc **10px** (`rounded-[10px]`), padding 16px, `AlertTitle` font semiBold 13px, `AlertDescription` font 12px `text-secondary`.

### 2. `StepProgress` (`src/components/ui/step-progress.tsx`)
* Hiển thị tiến trình 4 bước (hoặc danh sách bước động): `Create password` → `Save recovery key` → `Confirm` → `Ready`.
* Đường rail line chuẩn **3px** (`rounded-full`):
  * Bước đang active: Kéo dài thành **56px** với nền `action-primary` (`#c6ff34` Lime) và shadow dạ quang nhẹ.
  * Bước đã hoàn thành: Chiều dài **32px** với nền `action-primary`.
  * Bước chưa tới: Chiều dài **32px** với nền `border-default`.

### 3. `LoadingStatus` (`src/components/ui/loading-status.tsx`)
* Component hiển thị trạng thái tải / kiểm tra bảo mật trước khi mount workbench.
* Chiều cao **48px**, bo góc **12px**, nền `surface-raised` (`#23212c`), icon `LoaderCircle` quay mượt mà với stroke 1.5px.

### 4. `RecoveryKeyField` (`src/components/ui/recovery-key-field.tsx`)
* Component khóa bảo mật Vault chuyên dụng:
  * Trạng thái `Hidden`: Mật khẩu được che bằng ký tự `•••• •••• •••• •••• •••• ••••`, tiêu đề `RECOVERY KEY · SHOWN ONCE` màu `status-recovery` (`#e5bddf` Orchid).
  * Trạng thái `Revealed`: Hiển thị rõ key với font **Geist Mono** (`font-mono text-[15px]`).
  * Trạng thái `Copied`: Viền chuyển sang xanh `status-success` (`#99e1d9`), nhãn đổi thành `RECOVERY KEY · COPIED` và thông báo sao chép an toàn.
  * Tích hợp 2 nút thao tác: **Copy** (sao chép clipboard) và **Download** (tải file `.txt` offline) đều kèm Tooltip.

### 5. `SecurityNotice` (`src/components/ui/security-notice.tsx`)
* 2 loại thông báo an ninh:
  * `type="secure"`: Nền `status-success/10`, viền `status-success`, icon `Shield`.
  * `type="irreversible"`: Nền `surface-raised`, viền `border-default`, thanh nhấn viền 3px màu `status-recovery`, icon `TriangleAlert`.

### 6. `VaultShell` (`src/components/ui/vault-shell.tsx`)
* Khung container kích thước chuẩn **960x540px** cho quy trình Vault Gate / Setup:
  * Header chuẩn **30px** (`h-[30px]`): Biểu tượng `Vault` (14px) + "DevKit Vault" + nhãn `LOCAL · ENCRYPTED` màu `status-success`.
  * Slot nội dung trung tâm 640x380px bo góc 18px (`rounded-[18px]`).

#### Kiểm thử:
* Đã thực thi `bun run build:dev`, toàn bộ 6 components mới và cập nhật đều biên dịch thành công 100%.

---

## [2026-08-16] — Giai đoạn 4: Hoàn thiện màn hình 10 Vault & First-run

Đã triển khai hoàn chỉnh toàn bộ màn hình **Vault & First-run Gate** theo đúng thông số kỹ thuật Figma Trang 10 (`10 Vault & First-run`) kết hợp quy tắc nghiệp vụ từ `base-doc/content/projects/dev-kit/vault.mdx`:

### 1. Kiến trúc nghiệp vụ Vault (`base-doc` Business Contract)
* **Quy chuẩn bảo mật:** Không lưu trữ plaintext Master Password hoặc DEK trong log; Recovery Key chỉ được sinh và hiển thị **duy nhất 1 lần** trong quá trình setup lần đầu (`SetupVault`).
* **Trạng thái cổng VaultGate:**
  * `not_configured`: Kích hoạt luồng First-run setup 4 bước.
  * `locked`: Kích hoạt luồng Unlock bằng Master Password hoặc Khóa phục hồi khẩn cấp (`UnlockVaultWithRecovery`).
  * `unlocked`: Giữ DEK trong bộ nhớ tiến trình (`process memory`) cho phiên làm việc hiện tại, mở toàn quyền truy cập Workbench.

### 2. Cập nhật Bridge Adapter (`src/bridge/vault.ts`)
* Bổ sung các phương thức kết nối Wails / Go backend:
  * `vaultStatus()`: Lấy trạng thái hiện tại (`not_configured` | `locked` | `unlocked`).
  * `setupVault(masterPassword)`: Khởi tạo Master Password (≥ 8 ký tự), tạo keyring và trả về recovery key duy nhất.
  * `unlockVault(masterPassword)`: Mở khóa vault bằng mật khẩu.
  * `unlockVaultWithRecovery(recoveryKey)`: Mở khóa khẩn cấp bằng 29-ký tự recovery key.
  * `lockVault()`: Khóa tức thì và zeroize DEK trong memory.

### 3. Giao diện toàn diện VaultGate (`src/workbench/VaultGate.tsx`)
* **Window Title Bar (30px standard):**
  * Left: Biểu tượng `Vault` (14px) + "Vault" (`text-secondary`).
  * Center: Nhãn ngữ cảnh thay đổi động theo từng bước (`SETUP 1 OF 4`, `SETUP 2 OF 4`, `SETUP 4 OF 4`, `SECURE ACCESS`).
  * Right: Nhãn `LOCAL ONLY` + biểu tượng `ShieldCheck` màu `status-success` (`#99e1d9`).
* **Vault Decision Panel (Glassmorphism Modal):**
  * Nền kính mờ `surface-glass-modal` (`rgba(35, 33, 44, 0.92)`), `backdrop-blur-[14px]`, viền `border-default`, bo góc `18px`, bóng đổ sâu `shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.48)]`.
* **Hỗ trợ đầy đủ 6 luồng màn hình Figma:**
  1. `checking`: Kiểm tra trạng thái lưu trữ an toàn với `LoadingStatus`.
  2. `setup_password`: Tạo mật khẩu Master (có toggle ẩn/hiện mắt, kiểm tra trùng khớp & độ dài ≥ 8 ký tự), thanh `StepProgress` bước 1, `SecurityNotice` loại `irreversible`.
  3. `setup_recovery`: Hiển thị khóa phục hồi một lần duy nhất qua `RecoveryKeyField` (kèm chức năng Copy clipboard & Download `.txt` an toàn), checkbox xác nhận bắt buộc "I have saved the recovery key", `StepProgress` bước 2.
  4. `setup_ready`: Màn hình chúc mừng hoàn tất với biểu tượng Check 32px, `StepProgress` bước 4, `SecurityNotice` loại `secure`, nút "Enter DevKit" mở thẳng vào Workbench.
  5. `unlock_password`: Màn hình chào mừng mở khóa cho người dùng cũ với trường nhập Master password, xử lý thông báo lỗi `Alert` (destructive) và nút chuyển sang mở bằng Recovery key.
  6. `unlock_recovery`: Màn hình mở khóa khẩn cấp bằng Recovery key với định dạng font monospace, cảnh báo `Alert` (warning) và tùy chọn quay lại nhập mật khẩu.

#### Kiểm thử & Verification:
* Đã thực thi `bun run build` + `bun run check:performance`, kết quả build production và ngân sách hiệu năng đạt chuẩn 100%.

---

## [2026-08-16] — Giai đoạn 5: Hoàn thiện Popup 11 Settings

Đã triển khai hoàn chỉnh cửa sổ **Settings Modal** theo đúng quy chuẩn thiết kế Figma Trang 11 (`11 Settings`) (giữ nguyên Title Bar theo yêu cầu):

### 1. Cấu trúc khung Modal Settings (`940x620px`)
* **Header (56px):** Tiêu đề `Settings` (`text-[15px] font-medium`) + Nút đóng `X` (`IconButton` 32x32px bo góc 8px kèm tooltip).
* **Divider:** Đường phân cách mảnh 1px `border-border-subtle`.
* **Sidebar Navigation (220px):**
  * Nền `surface-raised`, viền phải `border-border-subtle`.
  * 4 mục điều hướng: **Account**, **Appearance**, **Sync & Devices**, **Environments**.
  * Hiệu ứng Active: Thanh nhấn Lime 3px (`bg-action-primary`), nền mềm `bg-action-primary/10`, chữ `text-primary font-medium`.

### 2. Các tab chức năng chi tiết:
* **Tab 1: Account (`Settings / 01 Account`)**
  * Header: "Account" (28px semiBold) + "Identity used for sync. Managed by your identity provider — not stored locally."
  * Identity Card: Avatar chữ lồng (Initials "SY" trên nền tròn Lime `#c6ff34`), email hiển thị, chấm trạng thái xanh `status-success` ("Signed in — session active").
  * Thao tác: Nút "Sign out" (với icon LogOut) / "Sign in with browser".
  * Thông báo an ninh: `SecurityNotice` loại `secure` ("Password never leaves your browser" - PKCE).
* **Tab 2: Appearance (`Settings / 02 Appearance`)**
  * Header: "Appearance" (28px semiBold) + "DevKit is dark-first today. Light and System themes are planned for a future release."
  * Section Theme: Icon Sun + nhãn "THEME", bộ 3 nút Dark (Active), Light (Disabled / Coming soon), System (Disabled / Coming soon).
* **Tab 3: Sync & Devices (`Settings / 03 Sync & Devices`)**
  * Header: "Sync & Devices" + nút thao tác nhanh "Sync now" (kèm hiệu ứng xoay icon khi đang đồng bộ).
  * Danh sách thiết bị kết nối:
    * MacBook Pro (`Laptop` icon, "This device · Active now", huy hiệu `Current`).
    * Mac mini (office) (`Monitor` icon, "Last active 2 days ago", nút "Revoke").
    * Studio setup (`Monitor` icon, "Last active 19 days ago", nút "Revoke").
  * Thông báo an ninh: `SecurityNotice` loại `irreversible` ("Revoking a device invalidates its session").
* **Tab 4: Environments (`Settings / 04 Environments` & `05 New tag`)**
  * Header: "Environments" (28px semiBold) + "Env tags required by SSH, Database, and Kafka profiles. prod is locked and always requires confirmation."
  * Danh sách thẻ môi trường (Tag List) từ backend bridge: `dev`, `stg`, `prod` (kèm icon `LockKeyhole` và huy hiệu `REQUIRES CONFIRMATION`), và các custom tag có nút xóa `Trash2`.
  * Form thêm thẻ môi trường mới (Inline New Tag Form): Ô nhập tên thẻ font monospace, checkbox "Require confirmation before executing actions", nút "Cancel" và "Add environment".
  * Tích hợp MCP Proxy Install Panel (`McpInstallPanel`).

* **Kiến trúc Popup Modal Overlay:**
  * Thay vì render làm tab che mất nội dung bên dưới, `SettingsModal` được triển khai dưới dạng **Modal Popup Overlay** bọc lớp scrim mờ `bg-black/45 backdrop-blur-[12px]`.
  * Toàn bộ nội dung workspace (Notes, SSH, Database, v.v.) bên dưới vẫn được giữ nguyên và hiển thị mờ ảo qua lớp kính (Backdrop Blur) tuyệt đẹp.
  * Hỗ trợ phím tắt `⌘,` (hoặc `Ctrl+,`), phím `Esc`, và bấm ra ngoài scrim để đóng mở nhanh.

### 3. Cập nhật Layout & Title Bar màn hình Vault (`VaultGate.tsx`):
* **Khung viền cửa sổ đồng bộ Workbench:**
  * Bọc nội dung Vault trong khung card bo góc **`mx-1 mb-1 rounded-xl border border-border bg-card`** giống hệt giao diện chính của ứng dụng (Image 2).
  * Nền outer frame chuyển sang `bg-transparent` để hiển thị hiệu ứng vibrancy nguyên bản của macOS.
* **Xử lý tránh bị nút điều hướng macOS (Traffic Lights) đè:**
  * Bổ sung lắng nghe sự kiện Fullscreen từ `@wailsio/runtime`.
  * Tự động áp dụng `pl-20` (80px padding left) khi ở chế độ cửa sổ thông thường để tránh hoàn toàn cụm nút đỏ/vàng/xanh của macOS.
  * Chuyển về `pl-1` mượt mà khi vào chế độ toàn màn hình (`isFullscreen`).
  * Sử dụng cấu trúc Grid 3 cột (`minmax(0,1fr)_minmax(0,5fr)_minmax(0,1fr)`) chuẩn xác như Workbench Header.
  * Hỗ trợ kéo di chuyển cửa sổ với thuộc tính `--wails-draggable:drag`.

#### Kiểm thử:
* Đã thực thi `bun run build` + `bun run check:performance`, 100% Passed.
