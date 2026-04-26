import { useRef, useEffect, useCallback } from 'react'
import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection } from '@codemirror/view'
import { EditorState } from '@codemirror/state'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { bracketMatching, foldGutter, indentOnInput } from '@codemirror/language'
import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete'
import { searchKeymap, highlightSelectionMatches } from '@codemirror/search'
import { lintGutter, forEachDiagnostic, forceLinting, type Diagnostic } from '@codemirror/lint'
import { dslLanguage, dslAutocomplete } from '@/lib/codemirror/dsl-language'
import { dslLinterExtension } from '@/lib/codemirror/dsl-linter'
import { argusEditorTheme, argusHighlightStyle } from '@/lib/codemirror/dsl-theme'

interface DSLEditorProps {
  value: string
  onChange: (value: string) => void
  onSave?: () => void
  onDryRun?: () => void
  // FIX-243 Wave D — fired on Ctrl/Cmd+Enter. Triggers an immediate
  // validation pass (instead of waiting for the debounced linter).
  onValidateNow?: () => void
  // FIX-243 Wave D — fired on Ctrl/Cmd+Shift+F. Asks the host page to
  // call POST /policies/validate?format=true and replace the buffer.
  onFormat?: () => void
  onDiagnostics?: (diagnostics: Diagnostic[]) => void
  readOnly?: boolean
  className?: string
}

export function DSLEditor({
  value,
  onChange,
  onSave,
  onDryRun,
  onValidateNow,
  onFormat,
  onDiagnostics,
  readOnly,
  className,
}: DSLEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onChangeRef = useRef(onChange)
  const onSaveRef = useRef(onSave)
  const onDryRunRef = useRef(onDryRun)
  const onValidateNowRef = useRef(onValidateNow)
  const onFormatRef = useRef(onFormat)
  const onDiagnosticsRef = useRef(onDiagnostics)

  onChangeRef.current = onChange
  onSaveRef.current = onSave
  onDryRunRef.current = onDryRun
  onValidateNowRef.current = onValidateNow
  onFormatRef.current = onFormat
  onDiagnosticsRef.current = onDiagnostics

  const createEditor = useCallback(() => {
    if (!containerRef.current) return

    if (viewRef.current) {
      viewRef.current.destroy()
    }

    // FIX-243 Wave D — keybind remap:
    //   Mod-s             → save
    //   Mod-Enter         → validate-now (was: dry-run)
    //   Mod-Shift-Enter   → dry-run
    //   Mod-Shift-f       → auto-format
    const customKeymap = keymap.of([
      {
        key: 'Mod-s',
        run: () => {
          onSaveRef.current?.()
          return true
        },
      },
      {
        key: 'Mod-Enter',
        run: (view) => {
          // Force the debounced linter to run immediately, then notify
          // the host (so it can refresh any error-summary state, etc.).
          forceLinting(view)
          onValidateNowRef.current?.()
          return true
        },
      },
      {
        key: 'Mod-Shift-Enter',
        run: () => {
          onDryRunRef.current?.()
          return true
        },
      },
      {
        key: 'Mod-Shift-f',
        preventDefault: true,
        run: () => {
          onFormatRef.current?.()
          return true
        },
      },
    ])

    const state = EditorState.create({
      doc: value,
      extensions: [
        customKeymap,
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightActiveLine(),
        history(),
        foldGutter(),
        drawSelection(),
        indentOnInput(),
        bracketMatching(),
        closeBrackets(),
        highlightSelectionMatches(),
        lintGutter(),
        dslLanguage,
        dslAutocomplete,
        dslLinterExtension(500),
        argusEditorTheme,
        argusHighlightStyle,
        keymap.of([
          ...closeBracketsKeymap,
          ...defaultKeymap,
          ...searchKeymap,
          ...historyKeymap,
          indentWithTab,
        ]),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onChangeRef.current(update.state.doc.toString())
          }
          if (onDiagnosticsRef.current) {
            const collected: Diagnostic[] = []
            forEachDiagnostic(update.state, (d) => {
              collected.push(d)
            })
            const prevCollected: Diagnostic[] = []
            forEachDiagnostic(update.startState, (d) => {
              prevCollected.push(d)
            })
            const changed =
              collected.length !== prevCollected.length ||
              collected.some(
                (d, i) =>
                  prevCollected[i] === undefined ||
                  prevCollected[i].from !== d.from ||
                  prevCollected[i].to !== d.to ||
                  prevCollected[i].severity !== d.severity ||
                  prevCollected[i].message !== d.message,
              )
            if (changed) {
              onDiagnosticsRef.current(collected)
            }
          }
        }),
        EditorState.readOnly.of(!!readOnly),
        EditorView.lineWrapping,
      ],
    })

    viewRef.current = new EditorView({
      state,
      parent: containerRef.current,
    })
  }, [value, readOnly])

  useEffect(() => {
    createEditor()
    return () => {
      viewRef.current?.destroy()
      viewRef.current = null
    }
  }, [readOnly])

  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    const currentDoc = view.state.doc.toString()
    if (currentDoc !== value) {
      view.dispatch({
        changes: {
          from: 0,
          to: currentDoc.length,
          insert: value,
        },
      })
    }
  }, [value])

  return (
    <div
      ref={containerRef}
      className={`h-full overflow-auto ${className ?? ''}`}
    />
  )
}
