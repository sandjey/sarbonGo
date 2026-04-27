--
-- PostgreSQL database dump
--

\restrict AotJbegimwPEDGHNtCJsPQm2ey9HgItKnrkkoF7bcys1QrdBX3u3cOKihPpYd7F

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
-- Name: call_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.call_events (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    call_id uuid NOT NULL,
    actor_id uuid,
    event_type character varying(50) NOT NULL,
    payload jsonb,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: call_events call_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.call_events
    ADD CONSTRAINT call_events_pkey PRIMARY KEY (id);


--
-- Name: idx_call_events_call_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_call_events_call_created ON public.call_events USING btree (call_id, created_at DESC);


--
-- Name: call_events call_events_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.call_events
    ADD CONSTRAINT call_events_call_id_fkey FOREIGN KEY (call_id) REFERENCES public.calls(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict AotJbegimwPEDGHNtCJsPQm2ey9HgItKnrkkoF7bcys1QrdBX3u3cOKihPpYd7F

