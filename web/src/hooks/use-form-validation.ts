import { useState, useCallback, useRef } from 'react'

type FieldValue = string | number | boolean | null | undefined

interface FieldRule {
  required?: boolean
  minLength?: number
  maxLength?: number
  pattern?: RegExp
  custom?: (value: FieldValue) => string | undefined
}

type Schema<T> = Partial<Record<keyof T, FieldRule>>
type Errors<T> = Partial<Record<keyof T, string>>
type Touched<T> = Partial<Record<keyof T, boolean>>

export function useFormValidation<T extends Record<string, FieldValue>>(
  initial: T,
  schema: Schema<T>,
) {
  const [values, setValues] = useState<T>(initial)
  const [errors, setErrors] = useState<Errors<T>>({})
  const [touched, setTouched] = useState<Touched<T>>({})
  const initialRef = useRef(initial)

  const validateField = useCallback(
    (name: keyof T, value: FieldValue): string | undefined => {
      const rule = schema[name]
      if (!rule) return undefined
      if (rule.required && (value === '' || value === null || value === undefined))
        return 'This field is required'
      if (typeof value === 'string') {
        if (rule.minLength && value.length < rule.minLength)
          return `Minimum ${rule.minLength} characters`
        if (rule.maxLength && value.length > rule.maxLength)
          return `Maximum ${rule.maxLength} characters`
        if (rule.pattern && !rule.pattern.test(value))
          return 'Invalid format'
      }
      return rule.custom?.(value)
    },
    [schema],
  )

  const handleChange = useCallback(
    (name: keyof T, value: FieldValue) => {
      setValues((prev) => ({ ...prev, [name]: value }))
      if (touched[name]) {
        const err = validateField(name, value)
        setErrors((prev) => ({ ...prev, [name]: err }))
      }
    },
    [touched, validateField],
  )

  const handleBlur = useCallback(
    (name: keyof T) => {
      setTouched((prev) => ({ ...prev, [name]: true }))
      const err = validateField(name, values[name])
      setErrors((prev) => ({ ...prev, [name]: err }))
    },
    [values, validateField],
  )

  const validateAll = useCallback(() => {
    const allTouched = Object.keys(values).reduce(
      (acc, k) => ({ ...acc, [k]: true }),
      {} as Touched<T>,
    )
    setTouched(allTouched)
    const allErrors = Object.keys(schema).reduce((acc, k) => {
      const err = validateField(k as keyof T, values[k as keyof T])
      if (err) acc[k as keyof T] = err
      return acc
    }, {} as Errors<T>)
    setErrors(allErrors)
    return Object.keys(allErrors).length === 0
  }, [values, schema, validateField])

  const reset = useCallback(() => {
    setValues(initialRef.current)
    setErrors({})
    setTouched({})
  }, [])

  const isDirty = JSON.stringify(values) !== JSON.stringify(initialRef.current)
  const isValid = Object.keys(errors).every((k) => !errors[k as keyof T])

  return { values, errors, touched, handleChange, handleBlur, validateAll, isValid, isDirty, reset, setValues }
}
