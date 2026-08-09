import type { Locale } from "./config";
import type en from "./dictionaries/en";

type WidenStrings<T> = {
  [Key in keyof T]: T[Key] extends string ? string : WidenStrings<T[Key]>;
};

export type Dictionary = WidenStrings<typeof en>;

const dictionaries = {
  en: () => import("./dictionaries/en").then((module) => module.default),
  ja: () => import("./dictionaries/ja").then((module) => module.default),
} satisfies Record<Locale, () => Promise<Dictionary>>;

export async function getDictionary(locale: Locale): Promise<Dictionary> {
  return dictionaries[locale]();
}
