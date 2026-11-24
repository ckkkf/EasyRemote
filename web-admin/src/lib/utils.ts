import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

/**
 * 合并 className：clsx 负责条件拼接，tailwind-merge 负责消解冲突的工具类。
 * shadcn primitives 统一通过它接收外部 className，避免同一视觉属性出现两个来源。
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
