import { describe, expect, it } from 'vitest';
import { normalizePath, utf8ByteLength, validateRelativePath } from './paths';

describe('validateRelativePath', () => {
  it('接受合同允许的相对路径', () => {
    expect(validateRelativePath('练习/1.pdf')).toBeNull();
    expect(validateRelativePath('试卷/学生版.docx')).toBeNull();
  });

  it('拒绝绝对路径、反斜杠与 ..', () => {
    expect(validateRelativePath('/etc/passwd')).not.toBeNull();
    expect(validateRelativePath('C:/paper.pdf')).not.toBeNull();
    expect(validateRelativePath('a\\b.pdf')).not.toBeNull();
    expect(validateRelativePath('../secret.pdf')).not.toBeNull();
    expect(validateRelativePath('a/../b.pdf')).not.toBeNull();
  });

  it('拒绝空段、控制字符与超长路径', () => {
    expect(validateRelativePath('a//b.pdf')).not.toBeNull();
    expect(validateRelativePath('a/')).not.toBeNull();
    expect(validateRelativePath('a/\u0000b.pdf')).not.toBeNull();
    expect(validateRelativePath('x'.repeat(1025))).not.toBeNull();
  });

  it('按 UTF-8 字节而不是字符计数', () => {
    expect(utf8ByteLength('档案')).toBe(6);
    expect(normalizePath('e\u0301.pdf')).toBe('é.pdf');
  });
});
