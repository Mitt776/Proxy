// Работа со списком профилей: загрузка, активация, обновление, удаление и
// вспомогательные действия (QR, копирование).
//
// Вынесено из ProfilesTab.svelte, когда появился второй экран профилей — на
// телефоне раскладка другая (список вместо сетки карточек, свои кнопки импорта,
// вместо правого клика — обычные кнопки), а действия над профилем те же самые.
// Держать их в двух компонентах — верный способ однажды починить удаление только
// в одном месте.
import { writable, get } from "svelte/store";
import {
  ListProfiles,
  GetActiveProfileID,
  SetActiveProfile,
  AddManualProfile,
  AddSubscriptionProfile,
  RefreshProfile,
  DeleteProfile,
  ProfileConfigJSON,
  ProfileRaw,
  ProfileQR,
} from "$api";
import { ClipboardSetText, ClipboardGetText } from "$api";
import type { profile } from "../../wailsjs/go/models";
import { reportError, showToast } from "./store";
import { tr } from "./i18n";

/** Список профилей и ID активного — общие для обоих экранов. */
export const profiles = writable<profile.Profile[]>([]);
export const activeProfileID = writable("");

/** loadProfiles перечитывает список у Go. Зовётся на монтировании и по profiles:changed. */
export async function loadProfiles() {
  try {
    const [list, id] = await Promise.all([ListProfiles(), GetActiveProfileID()]);
    profiles.set(list || []);
    activeProfileID.set(id);
  } catch (e) {
    reportError(e);
  }
}

/** activateProfile делает профиль активным. На живом соединении Go пересоберёт конфиг сам. */
export async function activateProfile(id: string) {
  try {
    await SetActiveProfile(id);
    activeProfileID.set(id);
  } catch (e) {
    reportError(e);
  }
}

export async function refreshProfile(id: string) {
  try {
    await RefreshProfile(id);
    await loadProfiles();
    showToast(tr("profiles.refreshed"));
  } catch (e) {
    reportError(e);
  }
}

export async function deleteProfile(id: string) {
  try {
    await DeleteProfile(id);
    await loadProfiles();
  } catch (e) {
    reportError(e);
  }
}

/**
 * addProfile добавляет профиль ссылками или подпиской. Ошибку не глотает и не
 * показывает сам: у обоих экранов она выводится прямо в форме добавления, рядом
 * с полем, а не всплывающим сообщением где-то ещё.
 */
export async function addProfile(mode: "manual" | "sub", value: string) {
  const v = value.trim();
  if (!v) throw new Error(mode === "manual" ? tr("profiles.needRaw") : tr("profiles.needURL"));
  if (mode === "manual") await AddManualProfile("", v);
  else await AddSubscriptionProfile("", v);
  await loadProfiles();
}

/** profileQR отдаёт data-URL с QR-кодом профиля (для показа на экране). */
export async function profileQR(id: string): Promise<string> {
  try {
    return await ProfileQR(id);
  } catch (e) {
    reportError(e);
    return "";
  }
}

export async function copyProfileJSON(id: string) {
  try {
    await ClipboardSetText(await ProfileConfigJSON(id));
    showToast(tr("profiles.copiedJSON"));
  } catch (e) {
    reportError(e);
  }
}

export async function copyProfileRaw(id: string) {
  try {
    await ClipboardSetText(await ProfileRaw(id));
    showToast(tr("profiles.copiedRaw"));
  } catch (e) {
    reportError(e);
  }
}

/** clipboardText — содержимое буфера обмена, обрезанное по краям («» если пусто). */
export async function clipboardText(): Promise<string> {
  return (await ClipboardGetText())?.trim() ?? "";
}

/** activeProfileName — имя активного профиля, если он есть. */
export function activeProfileName(): string {
  const id = get(activeProfileID);
  return get(profiles).find((p) => p.id === id)?.name ?? "";
}
