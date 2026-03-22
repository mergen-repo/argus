import { EditorView } from '@codemirror/view'
import { tags } from '@lezer/highlight'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'

export const argusEditorTheme = EditorView.theme({
  '&': {
    backgroundColor: '#06060B',
    color: '#E4E4ED',
    fontSize: '13px',
    fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
  },
  '.cm-content': {
    caretColor: '#00D4FF',
    padding: '12px 0',
  },
  '&.cm-focused .cm-cursor': {
    borderLeftColor: '#00D4FF',
    borderLeftWidth: '2px',
  },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': {
    backgroundColor: 'rgba(0,212,255,0.15) !important',
  },
  '.cm-activeLine': {
    backgroundColor: 'rgba(30,30,46,0.5)',
  },
  '.cm-gutters': {
    backgroundColor: '#0C0C14',
    color: '#4A4A65',
    border: 'none',
    borderRight: '1px solid #1E1E30',
  },
  '.cm-activeLineGutter': {
    backgroundColor: 'rgba(30,30,46,0.5)',
    color: '#7A7A95',
  },
  '.cm-lineNumbers .cm-gutterElement': {
    padding: '0 12px 0 8px',
    fontSize: '12px',
  },
  '.cm-matchingBracket': {
    backgroundColor: 'rgba(0,212,255,0.2)',
    outline: '1px solid rgba(0,212,255,0.4)',
  },
  '.cm-foldGutter': {
    width: '16px',
  },
  '.cm-tooltip': {
    backgroundColor: '#12121C',
    border: '1px solid #1E1E30',
    borderRadius: '6px',
    color: '#E4E4ED',
  },
  '.cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]': {
    backgroundColor: 'rgba(0,212,255,0.15)',
    color: '#E4E4ED',
  },
  '.cm-panels': {
    backgroundColor: '#0C0C14',
    borderBottom: '1px solid #1E1E30',
  },
  '.cm-panels.cm-panels-top': {
    borderBottom: '1px solid #1E1E30',
  },
  '.cm-searchMatch': {
    backgroundColor: 'rgba(255,184,0,0.2)',
    outline: '1px solid rgba(255,184,0,0.4)',
  },
  '.cm-searchMatch.cm-searchMatch-selected': {
    backgroundColor: 'rgba(0,212,255,0.25)',
  },
  '.cm-lintRange-error': {
    backgroundImage: 'none',
    borderBottom: '2px wavy #FF4466',
  },
  '.cm-lintRange-warning': {
    backgroundImage: 'none',
    borderBottom: '2px wavy #FFB800',
  },
  '.cm-diagnostic-error': {
    borderLeft: '3px solid #FF4466',
    backgroundColor: 'rgba(255,68,102,0.08)',
    color: '#E4E4ED',
  },
  '.cm-diagnostic-warning': {
    borderLeft: '3px solid #FFB800',
    backgroundColor: 'rgba(255,184,0,0.08)',
    color: '#E4E4ED',
  },
}, { dark: true })

export const argusHighlightStyle = syntaxHighlighting(HighlightStyle.define([
  { tag: tags.keyword, color: '#FF79C6', fontWeight: 'bold' },
  { tag: tags.string, color: '#F1FA8C' },
  { tag: tags.number, color: '#BD93F9' },
  { tag: tags.comment, color: '#6272A4', fontStyle: 'italic' },
  { tag: tags.function(tags.variableName), color: '#50FA7B' },
  { tag: tags.operator, color: '#FF6E6E' },
  { tag: tags.typeName, color: '#8BE9FD' },
  { tag: tags.variableName, color: '#E4E4ED' },
  { tag: tags.bracket, color: '#7A7A95' },
  { tag: tags.punctuation, color: '#7A7A95' },
]))
