// Minimal i18n: vi + en bundles. Default resolves from the game locale (vi→vi,
// else en); a manual toggle overrides and persists (plan §i18n).

export type Lang = "vi" | "en";

const STORE_KEY = "aqua_lang";

const en = {
  appName: "Aqua",
  // connection
  connected: "Connected",
  reconnecting: "Reconnecting",
  disconnected: "Disconnected",
  offline: "PC offline",
  // game states
  state_offline: "PC offline",
  state_menus: "In menus",
  state_lobby: "In lobby",
  state_queue: "In queue",
  state_matchfound: "Match found",
  state_pregame: "Agent select",
  state_locked: "Locked in",
  state_ingame: "In match",
  state_error: "Error",
  // status line
  noMatch: "Not in a match",
  // roles filter
  roleAll: "All",
  // grid / tiles
  recent: "Recent",
  notOwned: "Not owned",
  taken: "Taken",
  lockedByAlly: "Locked",
  // action bar
  armPrepick: "Arm pre-pick",
  prepickArmed: "Pre-pick armed",
  disarm: "Disarm",
  pickAnAgent: "Pick an agent",
  selectAgent: "Select an agent",
  lock: "Lock",
  holdToLock: "Hold to lock",
  cancel: "Cancel",
  confirmLock: "Tap again to lock",
  locking: "Locking…",
  lockedIn: "Locked in",
  autoLock: "Auto-lock",
  takenPickAnother: "was taken — pick another",
  banWarning: "Auto-lock is automation Riot can ban for.",
  // allies
  allies: "Allies",
  empty: "Open",
  you: "You",
  picking: "Picking…",
  // Lowercase summary labels for the collapsed allies count line.
  aLocked: "locked",
  aPicking: "picking",
  aOpen: "open",
  // in-match scoreboard
  teamAlly: "Your team",
  teamEnemy: "Enemy",
  statKd: "K/D",
  statAdr: "ADR",
  statHs: "HS%",
  statWin: "Win%",
  peak: "Peak",
  unranked: "Unranked",
  matchesShort: "matches",
  noStats: "No recent data",
  loadingStats: "Loading stats…",
  // offline / error screens
  pcOfflineTitle: "PC offline",
  pcOfflineBody: "Start Aqua.exe on your PC and open VALORANT.",
  errorTitle: "Something went off",
  errorBody: "The PC reported an error reading the game. It will retry.",
  waitingGame: "Waiting for VALORANT…",
  // pairing
  pairTitle: "Pair this phone",
  pairBody: "Scan the QR on your PC, or open its pairing link.",
  pairButton: "Pair",
  pairing: "Pairing…",
  pairFailed: "Pairing failed",
  needLink: "Open the pairing link shown on your PC.",
  forget: "Unpair this phone",
  // loading
  loading: "Connecting…",
  loadingCatalog: "Loading agents…",
};

export type TKey = keyof typeof en;
type Bundle = Record<TKey, string>;

const vi: Bundle = {
  appName: "Aqua",
  connected: "Đã kết nối",
  reconnecting: "Đang kết nối lại",
  disconnected: "Mất kết nối",
  offline: "PC ngoại tuyến",
  state_offline: "PC ngoại tuyến",
  state_menus: "Đang ở menu",
  state_lobby: "Trong sảnh",
  state_queue: "Đang tìm trận",
  state_matchfound: "Đã tìm thấy trận",
  state_pregame: "Chọn tướng",
  state_locked: "Đã khóa",
  state_ingame: "Trong trận",
  state_error: "Lỗi",
  noMatch: "Chưa vào trận",
  roleAll: "Tất cả",
  recent: "Gần đây",
  notOwned: "Chưa sở hữu",
  taken: "Đã bị chọn",
  lockedByAlly: "Đã khóa",
  armPrepick: "Đặt chọn trước",
  prepickArmed: "Đã đặt chọn trước",
  disarm: "Hủy",
  pickAnAgent: "Chọn một tướng",
  selectAgent: "Chọn một tướng",
  lock: "Khóa",
  holdToLock: "Giữ để khóa",
  cancel: "Hủy bỏ",
  confirmLock: "Nhấn lần nữa để khóa",
  locking: "Đang khóa…",
  lockedIn: "Đã khóa",
  autoLock: "Tự khóa",
  takenPickAnother: "đã bị chọn — hãy chọn tướng khác",
  banWarning: "Tự khóa là tự động hóa, Riot có thể cấm tài khoản.",
  allies: "Đồng đội",
  empty: "Trống",
  you: "Bạn",
  picking: "Đang chọn…",
  aLocked: "đã khóa",
  aPicking: "đang chọn",
  aOpen: "trống",
  teamAlly: "Đội bạn",
  teamEnemy: "Đối thủ",
  statKd: "K/D",
  statAdr: "ADR",
  statHs: "HS%",
  statWin: "Tỉ lệ thắng",
  peak: "Cao nhất",
  unranked: "Chưa xếp hạng",
  matchesShort: "trận",
  noStats: "Chưa có dữ liệu",
  loadingStats: "Đang tải chỉ số…",
  pcOfflineTitle: "PC ngoại tuyến",
  pcOfflineBody: "Mở Aqua.exe trên PC và khởi động VALORANT.",
  errorTitle: "Có gì đó trục trặc",
  errorBody: "PC báo lỗi khi đọc trạng thái game. Sẽ thử lại.",
  waitingGame: "Đang chờ VALORANT…",
  pairTitle: "Ghép nối điện thoại",
  pairBody: "Quét mã QR trên PC, hoặc mở liên kết ghép nối.",
  pairButton: "Ghép nối",
  pairing: "Đang ghép nối…",
  pairFailed: "Ghép nối thất bại",
  needLink: "Mở liên kết ghép nối hiển thị trên PC.",
  forget: "Hủy ghép nối điện thoại này",
  loading: "Đang kết nối…",
  loadingCatalog: "Đang tải tướng…",
};

const bundles: Record<Lang, Bundle> = { en, vi };

/** Resolve the initial language: stored manual choice wins, else game locale. */
export function resolveLang(gameLocale: string | undefined): Lang {
  const stored = localStorage.getItem(STORE_KEY);
  if (stored === "vi" || stored === "en") return stored;
  if (gameLocale && gameLocale.toLowerCase().startsWith("vi")) return "vi";
  return "en";
}

export function persistLang(lang: Lang): void {
  try {
    localStorage.setItem(STORE_KEY, lang);
  } catch {
    // ignore
  }
}

/** Whether the user has made a manual language choice (stops locale auto-switch). */
export function hasManualLang(): boolean {
  const s = localStorage.getItem(STORE_KEY);
  return s === "vi" || s === "en";
}

export function t(lang: Lang, key: TKey): string {
  return bundles[lang][key] ?? bundles.en[key] ?? key;
}
