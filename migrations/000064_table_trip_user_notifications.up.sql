--
-- PostgreSQL database dump
--

\restrict zQ9Gxw99OAyETQfYPDDsibKLTlRBKKbWN96KWRg0XtnSV2KpgSXLCdHKU5mAPlJ

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: trip_user_notifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.trip_user_notifications (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    trip_id uuid,
    recipient_kind character varying(16) NOT NULL,
    recipient_id uuid NOT NULL,
    event_kind character varying(64) NOT NULL,
    from_status character varying(32),
    to_status character varying(32),
    read_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    event_type character varying(32),
    payload jsonb,
    CONSTRAINT trip_user_notifications_recipient_kind_check CHECK (((recipient_kind)::text = ANY ((ARRAY['driver'::character varying, 'dispatcher'::character varying])::text[])))
);


--
-- Name: trip_user_notifications trip_user_notifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_user_notifications
    ADD CONSTRAINT trip_user_notifications_pkey PRIMARY KEY (id);


--
-- Name: idx_tun_recipient_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tun_recipient_created ON public.trip_user_notifications USING btree (recipient_kind, recipient_id, created_at DESC);


--
-- Name: idx_tun_recipient_type_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tun_recipient_type_created ON public.trip_user_notifications USING btree (recipient_kind, recipient_id, event_type, created_at DESC);


--
-- Name: idx_tun_trip; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tun_trip ON public.trip_user_notifications USING btree (trip_id);


--
-- Name: idx_tun_unread; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tun_unread ON public.trip_user_notifications USING btree (recipient_kind, recipient_id) WHERE (read_at IS NULL);


--
-- PostgreSQL database dump complete
--

\unrestrict zQ9Gxw99OAyETQfYPDDsibKLTlRBKKbWN96KWRg0XtnSV2KpgSXLCdHKU5mAPlJ

